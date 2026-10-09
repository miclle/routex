package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testTeamQuotaNotificationLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{117}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	var held, unknown, truncated atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if held.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if truncated.Load() {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Interrupted\"},\"finish_reason\":null}]}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Unknown usage"},"finish_reason":"stop"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	var instances []*service.Service
	defer func() {
		unblock()
		for _, instance := range instances {
			instance.StopRuntime()
			_ = instance.StopCallRecorder()
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		instance, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
		return instance
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-notice-admin@example.invalid","password":"team-notification-password","name":"Notification administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatalf("seed %T: %v", row, err)
			}
		}
	}
	request := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", csrf)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	modelID := "mdl_team_notice"
	bearer := "rx_" + strings.Repeat("n", 43)
	cipher, err := store.Seal("crd_team_notice", "disposable-notification-upstream")
	if err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Provider{ID: "prv_team_notice", Name: "Notification provider"},
		&entity.ProviderConnection{ID: "con_team_notice", ProviderID: "prv_team_notice", Name: "Native Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_notice", ConnectionID: "con_team_notice", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_notice", ConnectionID: "con_team_notice", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_notice", ProviderModelID: "pmd_team_notice"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-notice-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_team_notice", ModelID: modelID, ProviderModelID: "pmd_team_notice", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_team_notice_warm", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_notice_warm", ModelID: modelID},
		&entity.ModelPrice{ID: "price_team_notice", ProviderModelID: "pmd_team_notice", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_team_notice", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bnd_team_notice", Evidence: "Controlled native maximum", Reason: "Team notice acceptance"},
	)
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: fmt.Sprintf("rate_team_notice_%d", index), ModelPriceID: "price_team_notice", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	// This Team predates actual journal activation, so zero usage lacks coverage.
	create(&entity.Team{ID: "tea_notice_uncovered", Name: "Before coverage", Status: entity.ResourceActive, CreatedAt: time.Now().UTC().Add(-time.Hour)})
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "team-notifications.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"team-notice-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmup.Header.Set("Content-Type", "application/json")
	warmupResponse := httptest.NewRecorder()
	router.ServeHTTP(warmupResponse, warmup)
	expectStatus(t, warmupResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	users := map[string]entity.User{}
	for _, name := range []string{"owner", "caller", "peer", "disabled", "inactive", "offboarded", "operator", "new"} {
		member, err := svc.CreateMember(ctx, admin.User.ID, "team-notice-"+name+"@example.invalid", "team-notification-password", "Notice "+name, entity.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		users[name] = member.User
	}
	offboardedAt := time.Now().UTC()
	if err := db.Model(&entity.User{}).Where("id = ?", users["disabled"].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users["offboarded"].ID).Update("offboarded_at", offboardedAt).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.Role{ID: "rol_team_notice_ops", Name: "Notification operator", NameKey: "team-notice-operator"}, &entity.RolePermission{RoleID: "rol_team_notice_ops", Permission: "system.read"}, &entity.UserRole{UserID: users["operator"].ID, RoleID: "rol_team_notice_ops"})
	teamID := "tea_notice_main"
	create(&entity.Team{ID: teamID, Name: "Frozen Team notice name", Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	for _, name := range []string{"owner", "caller", "peer", "disabled", "inactive", "offboarded"} {
		role, status := entity.TeamMember, entity.ResourceActive
		if name == "owner" {
			role = entity.TeamOwner
		}
		if name == "inactive" {
			status = entity.ResourceDisabled
		}
		create(&entity.TeamMembership{ID: "tmm_notice_" + name, TeamID: teamID, UserID: users[name].ID, Role: role, Status: status})
	}
	for _, suffix := range []string{"unknown", "truncated", "stale", "inactive"} {
		id := "tea_notice_" + suffix
		create(&entity.Team{ID: id, Name: "Suppressed " + suffix, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_notice_" + suffix + "_own", TeamID: id, UserID: users["owner"].ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_notice_" + suffix + "_call", TeamID: id, UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: id, ModelID: modelID})
	}
	create(&entity.TeamMembership{ID: "tmm_notice_uncovered", TeamID: "tea_notice_uncovered", UserID: users["owner"].ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
	// Raw aliases may be rejected by PostgreSQL FK checks. If persisted under a
	// case-insensitive schema, neither may become a Team recipient.
	for _, row := range []*entity.TeamMembership{
		{ID: "tmm_notice_alias_user", TeamID: teamID, UserID: strings.ToUpper(users["operator"].ID), Role: entity.TeamMember, Status: entity.ResourceActive},
		{ID: "tmm_notice_alias_team", TeamID: strings.ToUpper(teamID), UserID: users["new"].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
	} {
		if err := db.Create(row).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("alias seed", err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	type identity struct {
		csrf   string
		cookie *http.Cookie
	}
	identities := map[string]identity{}
	for _, name := range []string{"owner", "caller", "peer", "operator", "new"} {
		auth, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-notice-`+name+`@example.invalid","password":"team-notification-password"}`, nil, ""))
		identities[name] = identity{auth.CSRFToken, cookie}
	}
	actorRequest := func(name, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		actor := identities[name]
		return request(actor.cookie, actor.csrf, method, path, nil)
	}
	childAccount, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, users["caller"].ID)
	if err != nil || childAccount == nil || !strings.HasPrefix(childAccount.AccountID, "team_member_") {
		t.Fatal("missing canonical child account", err)
	}
	childScope := strings.TrimPrefix(childAccount.AccountID, "team_member_")
	if len(childScope) != 52 {
		t.Fatal("invalid canonical child scope", childScope)
	}
	var childRevision string
	privateSnapshots := map[string]string{}
	warningSnapshots := map[string]string{}
	warningRecipientBirths := map[string]time.Time{}
	warningRecipientIDs := make([]string, 0, len(users))
	for _, user := range users {
		warningRecipientIDs = append(warningRecipientIDs, user.ID)
	}
	var originalWarningRecipients []entity.User
	if err := db.Where("id IN ?", warningRecipientIDs).Find(&originalWarningRecipients).Error; err != nil || len(originalWarningRecipients) != len(users) {
		t.Fatal("original warning recipient identities incomplete", err)
	}
	for _, user := range originalWarningRecipients {
		warningRecipientBirths[user.ID] = user.CreatedAt
	}
	warningDecimal := regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	page := func(name string) service.NotificationPage {
		t.Helper()
		// Validate the entire real response before projecting aggregate-only expectations.
		raw := decodeCatalogResponse[service.NotificationPage](t, actorRequest(name, "GET", "/api/v1/notifications?status=all&limit=100"), 200)
		if raw.NextCursor != "" {
			t.Fatal("complete fixture inbox unexpectedly exceeded one bounded page")
		}
		var rawUnread int64
		seen := map[string]bool{}
		projection := service.NotificationPage{Items: []service.NotificationRecord{}}
		for _, record := range raw.Items {
			if seen[record.ID] || record.Read != (record.ReadAt != nil) {
				t.Fatal("raw inbox identity/read flag mismatch", record.ID)
			}
			seen[record.ID] = true
			if !record.Read {
				rawUnread++
			}
			if record.Kind == "monthly_quota_warning" || record.QuotaWarning != nil || record.QuotaWarningObservationID != "" {
				q := record.QuotaWarning
				if record.Kind != "monthly_quota_warning" || q == nil || q.ScopeKind != "team" || q.ScopeID != teamID || record.SubjectType != "team" || record.SubjectID != q.ScopeID || !strings.HasPrefix(record.ID, "twi_") || !strings.HasPrefix(record.QuotaWarningObservationID, "two_") || record.Quota != nil || record.QuotaObservationID != "" || record.AlertID != "" || record.OccurrenceCount != 1 || record.DeliveryStatus != "" || record.DeliveryCode != "" || record.DeliveryAttempts != 0 || record.DeliveryUpdatedAt != nil {
					t.Fatal("unexpected mixed Team warning shape", record)
				}
				var inbox entity.TeamQuotaWarningInbox
				if err := db.First(&inbox, "id = ?", record.ID).Error; err != nil || inbox.ID != record.ID || inbox.RecipientID != users[name].ID || inbox.ObservationID != record.QuotaWarningObservationID || inbox.RecipientCreatedAt.IsZero() || !inbox.RecipientCreatedAt.Equal(warningRecipientBirths[users[name].ID]) || (inbox.ReadAt != nil) != record.Read || (inbox.ReadAt != nil && !inbox.ReadAt.Equal(*record.ReadAt)) || !inbox.CreatedAt.Equal(record.LastSeenAt) {
					t.Fatal("mixed Team warning borrowed original recipient/read receipt", record.ID, err)
				}
				var observation entity.TeamQuotaWarningObservation
				if err := db.First(&observation, "id = ?", inbox.ObservationID).Error; err != nil || observation.ID != inbox.ObservationID || observation.TeamID != teamID || observation.ThresholdGeneration != "team-monthly-80-90-v1" || observation.ResourceCreatedAt.IsZero() || observation.PolicyRevision == "" || observation.TimeZone == "" || !observation.MonthEnd.After(observation.MonthStart) || observation.AsOf.Before(observation.MonthStart) || !observation.AsOf.Before(observation.MonthEnd) || observation.AsOf.Before(observation.ResourceCreatedAt) || observation.AsOf.Before(observation.CoverageStart) {
					t.Fatal("mixed Team warning observation source mismatch", record.ID, err)
				}
				location, err := time.LoadLocation(observation.TimeZone)
				if err != nil {
					t.Fatal("mixed Team warning calendar unavailable", record.ID, err)
				}
				local := observation.AsOf.In(location)
				month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
				coveredFrom := observation.MonthStart
				if observation.ResourceCreatedAt.After(coveredFrom) {
					coveredFrom = observation.ResourceCreatedAt
				}
				if !observation.MonthStart.Equal(month) || !observation.MonthEnd.Equal(month.AddDate(0, 1, 0)) || observation.CoverageStart.IsZero() || observation.CoverageStart.After(coveredFrom) {
					t.Fatal("mixed Team warning lacks complete recorded month coverage", record.ID)
				}
				var actor entity.User
				var team entity.Team
				if err := db.First(&actor, "id = ?", inbox.RecipientID).Error; err != nil || actor.ID != inbox.RecipientID || actor.Disabled || actor.OffboardedAt != nil || actor.ApprovalApplicationID != nil || !actor.CreatedAt.Equal(inbox.RecipientCreatedAt) {
					t.Fatal("mixed Team warning current exact actor/birth mismatch", record.ID, err)
				}
				if err := db.First(&team, "id = ?", observation.TeamID).Error; err != nil || team.ID != observation.TeamID || team.Status != entity.ResourceActive || !team.CreatedAt.Equal(observation.ResourceCreatedAt) {
					t.Fatal("mixed Team warning current exact Team/birth mismatch", record.ID, err)
				}
				var memberships []entity.TeamMembership
				if err := db.Where("team_id = ? AND user_id = ?", team.ID, actor.ID).Limit(1001).Find(&memberships).Error; err != nil || len(memberships) > 1000 {
					t.Fatal("mixed Team warning current membership read incomplete", record.ID, err)
				}
				var currentMemberships int
				for _, membership := range memberships {
					if membership.TeamID == team.ID && membership.UserID == actor.ID && membership.Status == entity.ResourceActive && (membership.Role == entity.TeamOwner || membership.Role == entity.TeamMember) {
						currentMemberships++
					}
				}
				if currentMemberships != 1 {
					t.Fatal("mixed Team warning lacks one current canonical membership", record.ID, currentMemberships)
				}
				severity := "medium"
				if observation.Level == "critical" {
					severity = "high"
				}
				if (observation.Level != "near" || observation.Threshold != 80) && (observation.Level != "critical" || observation.Threshold != 90) || record.Severity != severity || record.DetailCode != observation.Dimension+"_month_"+observation.Level || !warningDecimal.MatchString(observation.Limit) || !warningDecimal.MatchString(observation.Settled) || (observation.Dimension == "tokens" && (observation.Currency != "" || q.Currency != nil || strings.Contains(observation.Limit, ".") || strings.Contains(observation.Settled, "."))) || (observation.Dimension == "money" && (observation.Currency != "USD" || q.Currency == nil || *q.Currency != observation.Currency)) || (observation.Dimension != "tokens" && observation.Dimension != "money") {
					t.Fatal("mixed Team warning dimension/level/currency mismatch", record.ID)
				}
				used, usedOK := new(big.Rat).SetString(observation.Settled)
				limit, limitOK := new(big.Rat).SetString(observation.Limit)
				if !usedOK || !limitOK || limit.Sign() <= 0 || used.Sign() < 0 || used.Cmp(limit) >= 0 || new(big.Rat).Mul(used, big.NewRat(100, 1)).Cmp(new(big.Rat).Mul(limit, big.NewRat(int64(observation.Threshold), 1))) < 0 || (observation.Level == "near" && new(big.Rat).Mul(used, big.NewRat(100, 1)).Cmp(new(big.Rat).Mul(limit, big.NewRat(90, 1))) >= 0) {
					t.Fatal("mixed Team warning is not the exact highest below-exhaustion level", record.ID)
				}
				if q.Dimension != observation.Dimension || q.PolicyRevision != observation.PolicyRevision || !q.MonthStart.Equal(observation.MonthStart) || !q.MonthEnd.Equal(observation.MonthEnd) || q.TimeZone != observation.TimeZone || !q.AsOf.Equal(observation.AsOf) || q.Limit != observation.Limit || q.Settled != observation.Settled || q.Level != observation.Level || q.Threshold != observation.Threshold || q.ThresholdGeneration != observation.ThresholdGeneration || record.SubjectName != observation.TeamName || !record.FirstSeenAt.Equal(observation.AsOf) {
					t.Fatal("mixed Team warning snapshot differs from retained observation", record.ID)
				}
				immutable := record
				immutable.Read, immutable.ReadAt = false, nil
				encoded, err := json.Marshal(struct {
					Record      service.NotificationRecord
					Observation entity.TeamQuotaWarningObservation
				}{immutable, observation})
				if err != nil {
					t.Fatal(err)
				}
				if prior, found := warningSnapshots[record.ID]; found && prior != string(encoded) {
					t.Fatal("mixed Team warning replay changed immutable snapshot", record.ID)
				}
				warningSnapshots[record.ID] = string(encoded)
				continue // Only this fully validated warning is outside the exhaustion projection.
			}
			var recipient entity.QuotaNotificationInbox
			if err := db.First(&recipient, "id = ?", record.ID).Error; err != nil || recipient.ID != record.ID || recipient.RecipientID != users[name].ID || recipient.ObservationID != record.QuotaObservationID || (recipient.ReadAt != nil) != record.Read {
				t.Fatal("raw inbox borrowed recipient/read authority", record.ID, err)
			}
			if record.Quota == nil {
				t.Fatal("unexpected non-quota notice in isolated fixture", record.ID)
			}
			switch record.Quota.ScopeKind {
			case "team":
				projection.Items = append(projection.Items, record)
				if !record.Read {
					projection.UnreadCount++
				}
			case "team_member":
				q := record.Quota
				if name != "caller" || childRevision == "" || record.SubjectType != "team_member" || record.SubjectID != childScope || record.SubjectName != "Frozen Team notice name" || q.ScopeID != childScope || q.TeamID == nil || *q.TeamID != teamID || q.MemberUserID == nil || *q.MemberUserID != users["caller"].ID || q.PolicyRevision != childRevision || q.Limit != "5" || q.Settled != "5" || record.Kind != "monthly_quota_exhausted" || record.Severity != "high" || record.AlertID != "" || record.DeliveryStatus != "" || !q.MonthEnd.After(q.MonthStart) || q.AsOf.Before(q.MonthStart) || !q.AsOf.Before(q.MonthEnd) || q.TimeZone == "" {
					t.Fatal("private child snapshot or self-recipient mismatch", record)
				}
				if (q.Dimension == "tokens" && (q.Currency != nil || record.DetailCode != "tokens_month_exhausted")) || (q.Dimension == "money" && (q.Currency == nil || *q.Currency != "USD" || record.DetailCode != "money_month_exhausted")) || (q.Dimension != "tokens" && q.Dimension != "money") {
					t.Fatal("private child dimension/currency mismatch", record)
				}
				immutable := record
				immutable.Read, immutable.ReadAt = false, nil
				encoded, err := json.Marshal(immutable)
				if err != nil {
					t.Fatal(err)
				}
				if prior, found := privateSnapshots[record.ID]; found && prior != string(encoded) {
					t.Fatal("private child replay changed immutable snapshot", record.ID)
				}
				privateSnapshots[record.ID] = string(encoded)
			default:
				t.Fatal("Team fixture emitted Personal/Project or unknown quota scope", record.Quota.ScopeKind)
			}
		}
		if raw.UnreadCount != rawUnread {
			t.Fatalf("complete raw unread total mismatch: reported=%d actual=%d", raw.UnreadCount, rawUnread)
		}
		return projection
	}
	readLimit := func(team, user string) *service.LimitRecord {
		t.Helper()
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, team, user)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	writeLimit := func(team, user, patch string) *service.LimitRecord {
		t.Helper()
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		value, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, team, user, readLimit(team, user).ETag, input)
		if err != nil || !value.Enforced {
			t.Fatal("Team policy was not applied", value, err)
		}
		return value
	}
	native := func(team string, stream bool) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		payload := body
		if stream {
			payload = strings.TrimSuffix(body, "}") + `,"stream":true,"stream_options":{"include_usage":true}}`
		}
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/api/v1/teams/"+team+"/chat/completions", strings.NewReader(payload))
		req.AddCookie(identities["caller"].cookie)
		req.Header.Set("X-CSRF-Token", identities["caller"].csrf)
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	reconcileSavedSnapshot := func() {
		t.Helper()
		if err := svc.ReconcileMonthlyQuotaNotifications(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		reconcileSavedSnapshot()
	}
	count := func(team string) int64 {
		t.Helper()
		var value int64
		if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_kind = ? AND scope_id = ?", "team", team).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	quotaJSON := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("encode quota failure context", err)
		}
		return string(raw)
	}
	// Four independently rounded price components reserve two extra 18-decimal
	// units. Budget that bound before dispatch; actual settlement remains five.
	writeLimit(teamID, "", `{"tokens_month":5,"money_month":"5.000000000000000002","currency":"USD","reason":"Budget the conservative Team reservation"}`)
	writeLimit(teamID, users["caller"].ID, `{"tokens_month":5,"money_month":"5.000000000000000002","currency":"USD","reason":"Budget the conservative child reservation"}`)
	held.Store(true)
	heldResult := make(chan *httptest.ResponseRecorder, 1)
	go func() { heldResult <- native(teamID, false) }()
	select {
	case <-entered:
	case response := <-heldResult:
		t.Fatalf("Team hold returned before dispatch: status=%d body=%s aggregate=%s member=%s", response.Code, response.Body.String(), quotaJSON(readLimit(teamID, "")), quotaJSON(readLimit(teamID, users["caller"].ID)))
	case <-time.After(10 * time.Second):
		t.Fatalf("Team hold did not dispatch: aggregate=%s member=%s", quotaJSON(readLimit(teamID, "")), quotaJSON(readLimit(teamID, users["caller"].ID)))
	}
	for _, user := range []string{"", users["caller"].ID} {
		usage := readLimit(teamID, user).QuotaUsage
		if usage == nil || usage.Month == nil || usage.Active == nil || usage.Month.TokensUsed != 0 || usage.Month.TokensHeld != 0 || len(usage.Month.MoneyHeld) != 0 || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUnknown != 0 || usage.Active.TokensHeld != 5 || usage.Active.MoneyHeld["USD"] != "5.000000000000000002" || usage.Active.TokensUnknown != 0 || usage.Active.MoneyUnknown != 0 {
			t.Fatalf("one atomic aggregate/child reservation missing for %q: %s", user, quotaJSON(usage))
		}
	}
	reconcile()
	if count(teamID) != 0 {
		t.Fatal("holds created a settled-exhaustion observation")
	}
	unblock()
	held.Store(false)
	expectStatus(t, <-heldResult, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"", users["caller"].ID} {
		usage := readLimit(teamID, user).QuotaUsage
		if usage == nil || usage.Month == nil || usage.Active == nil || !usage.Month.Covered || usage.Month.TokensUsed != 5 || usage.Month.TokensHeld != 0 || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUsed["USD"] != "5" || usage.Month.MoneyUnknown != 0 || usage.Active.TokensHeld != 0 || len(usage.Active.MoneyHeld) != 0 || usage.Active.TokensUnknown != 0 || usage.Active.MoneyUnknown != 0 {
			t.Fatalf("settlement changed aggregate/child totals for %q: %s", user, quotaJSON(usage))
		}
	}
	var call entity.CallRecord
	if err := db.Where("team_id = ?", teamID).First(&call).Error; err != nil || call.UserID != users["caller"].ID || call.TeamMembershipID != "tmm_notice_caller" || call.KeyID != "" || call.ProjectID != "" || call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 {
		t.Fatal("native immutable Team call facts changed", call, err)
	}
	if call.ChargeAmount == nil || *call.ChargeAmount != "5" || call.ChargeCurrency == nil || *call.ChargeCurrency != "USD" {
		t.Fatal("native immutable Team charge changed", call)
	}
	var attempt entity.CallAttempt
	if err := db.Where("request_id = ?", call.RequestID).First(&attempt).Error; err != nil || attempt.NativeCompletionEvidence != "completed" {
		t.Fatal("normal path lacked native completed evidence", attempt, err)
	}
	// Observe settled exhaustion against a newly applied exact ceiling, not
	// against the conservative admission reservation or an unreviewed policy.
	initial := writeLimit(teamID, "", `{"money_month":"5","currency":"USD","reason":"Observe exact settled Team exhaustion"}`)
	writeLimit(teamID, users["caller"].ID, `{"money_month":"5","currency":"USD","reason":"Observe exact settled child exhaustion"}`)
	var childRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", childScope).First(&childRow).Error; err != nil || childRow.ScopeKind != "team_member" || childRow.ScopeID != childScope || childRow.ETag == "" || childRow.TokensMonth == nil || *childRow.TokensMonth != 5 || childRow.MoneyMonth == nil || *childRow.MoneyMonth != "5" || childRow.Currency != "USD" {
		t.Fatal("missing exact reviewed child policy", childRow, err)
	}
	childRevision = childRow.ETag
	// Team HTTP ETags review the complete authority/pricing context. Quota
	// snapshots retain the canonical stored policy revision independently.
	var initialRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team", teamID).First(&initialRow).Error; err != nil || initialRow.ScopeKind != "team" || initialRow.ScopeID != teamID || initialRow.ETag == "" || initialRow.TokensMonth == nil || *initialRow.TokensMonth != 5 || initialRow.MoneyMonth == nil || *initialRow.MoneyMonth != "5" || initialRow.Currency != "USD" || initial.Stored.TokensMonth == nil || *initial.Stored.TokensMonth != 5 || initial.Stored.MoneyMonth == nil || *initial.Stored.MoneyMonth != "5" || !initial.Enforced {
		t.Fatalf("current reviewed aggregate policy source changed: row=%s review=%s error=%v", quotaJSON(initialRow), quotaJSON(initial), err)
	}
	beforeDispatch := dispatches.Load()
	expectStatus(t, native(teamID, false), 429)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("notification extension changed finite Team enforcement")
	}
	personal, err := svc.GetResourceLimit(ctx, users["caller"].ID, service.LimitTarget{Kind: "user", ID: users["caller"].ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 || len(personal.QuotaUsage.Month.MoneyUsed) != 0 {
		t.Fatal("Team calls charged the Personal account", personal, err)
	}
	reconcile()
	reconcile()
	if count(teamID) != 2 {
		t.Fatal("exact Team token/money observations missing or duplicated")
	}
	var firstToken, firstMoney service.NotificationRecord
	for _, name := range []string{"owner", "caller", "peer"} {
		inbox := page(name)
		if len(inbox.Items) != 2 || inbox.UnreadCount != 2 {
			t.Fatalf("active %s recipient missing: %+v", name, inbox)
		}
		for _, record := range inbox.Items {
			if record.Quota == nil || record.Quota.ScopeKind != "team" || record.Quota.ScopeID != teamID || record.Quota.PolicyRevision != initialRow.ETag || record.Quota.Limit != "5" || record.Quota.Settled != "5" || record.Kind != "monthly_quota_exhausted" || record.Severity != "high" || record.SubjectName != "Frozen Team notice name" || record.AlertID != "" || record.DeliveryStatus != "" {
				t.Fatalf("Team snapshot changed for %s: expected_policy_revision=%s current_review_etag=%s record=%s", name, initialRow.ETag, initial.ETag, quotaJSON(record))
			}
			if record.Quota.Dimension == "money" {
				if record.Quota.Currency == nil || *record.Quota.Currency != "USD" || record.DetailCode != "money_month_exhausted" {
					t.Fatal("incorrect money notification", record)
				}
				if name == "caller" {
					firstMoney = record
				}
			} else {
				if record.Quota.Currency != nil || record.DetailCode != "tokens_month_exhausted" {
					t.Fatal("incorrect token notification", record)
				}
				if name == "caller" {
					firstToken = record
				}
			}
		}
	}
	var recipientIDs []string
	if err := db.Model(&entity.QuotaNotificationInbox{}).Distinct("recipient_id").Pluck("recipient_id", &recipientIDs).Error; err != nil {
		t.Fatal(err)
	}
	if len(recipientIDs) != 3 {
		t.Fatal("disabled, offboarded, inactive or aliased member received fanout", recipientIDs)
	}
	for _, name := range []string{"operator", "new"} {
		if inbox := page(name); len(inbox.Items) != 0 || inbox.UnreadCount != 0 {
			t.Fatal("nonmember received Team fanout", name, inbox)
		}
	}
	adminPage := decodeCatalogResponse[service.NotificationPage](t, request(adminCookie, admin.CSRFToken, "GET", "/api/v1/notifications?status=all", nil), 200)
	for _, record := range adminPage.Items {
		if record.Quota != nil {
			t.Fatal("administrator borrowed Team recipient authority", record)
		}
	}
	expectStatus(t, request(adminCookie, admin.CSRFToken, "POST", "/api/v1/notifications/"+firstToken.ID+"/read", nil), 404)
	expectStatus(t, actorRequest("owner", "POST", "/api/v1/notifications/"+firstToken.ID+"/read"), 404)
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+strings.ToUpper(firstToken.ID)+"/read"), 404)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/notifications/"+firstToken.ID+"/read", "", identities["caller"].cookie, ""), 403)
	expectStatus(t, actorRequest("caller", "GET", "/api/v1/notifications?recipient_id="+users["owner"].ID), 400)
	read := decodeCatalogResponse[service.NotificationRecord](t, actorRequest("caller", "POST", "/api/v1/notifications/"+firstToken.ID+"/read"), 200)
	readAgain := decodeCatalogResponse[service.NotificationRecord](t, actorRequest("caller", "POST", "/api/v1/notifications/"+firstToken.ID+"/read"), 200)
	readThird := decodeCatalogResponse[service.NotificationRecord](t, actorRequest("caller", "POST", "/api/v1/notifications/"+firstToken.ID+"/read"), 200)
	if !read.Read || read.ReadAt == nil || readAgain.ReadAt == nil || readThird.ReadAt == nil || !readAgain.ReadAt.Equal(*readThird.ReadAt) {
		t.Fatal("read retry reset timestamp")
	}
	// The persisted read receipt is the baseline at each driver's timestamp precision.
	read = readAgain
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).Update("name", "Later Team name").Error; err != nil {
		t.Fatal(err)
	}
	// Freeze recipient membership at observation time; new membership cannot replay.
	if err := db.Delete(&entity.TeamMembership{}, "id = ?", "tmm_notice_alias_team").Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.TeamMembership{ID: "tmm_notice_new", TeamID: teamID, UserID: users["new"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	reconcile()
	if len(page("new").Items) != 0 || count(teamID) != 2 {
		t.Fatal("replay added a newly joined recipient")
	}
	for _, record := range page("caller").Items {
		if record.SubjectName != "Frozen Team notice name" {
			t.Fatal("replay replaced frozen Team name")
		}
	}
	// Current authority gates list/count/read/read-all, independent of frozen rows.
	if err := db.Delete(&entity.TeamMembership{}, "id = ?", "tmm_notice_caller").Error; err != nil {
		t.Fatal(err)
	}
	if inbox := page("caller"); len(inbox.Items) != 0 || inbox.UnreadCount != 0 {
		t.Fatal("departed recipient retained Team history")
	}
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+firstMoney.ID+"/read"), 404)
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/read-all"), 204)
	var moneyInbox entity.QuotaNotificationInbox
	if err := db.First(&moneyInbox, "id = ?", firstMoney.ID).Error; err != nil || moneyInbox.ReadAt != nil {
		t.Fatal("read-all marked inaccessible historical Team row", err)
	}
	create(&entity.TeamMembership{ID: "tmm_notice_rejoined", TeamID: teamID, UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	if inbox := page("caller"); len(inbox.Items) != 2 || inbox.UnreadCount != 1 {
		t.Fatal("restored authority reset frozen recipient/read state", inbox)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if usage := readLimit(teamID, users["caller"].ID).QuotaUsage; usage == nil || usage.Month == nil || usage.Month.TokensUsed != 5 || usage.Month.MoneyUsed["USD"] != "5" {
		t.Fatal("rejoin reset stable child accounting", usage)
	}
	for _, mutation := range []struct {
		model          any
		id, field      string
		value, restore any
	}{
		{&entity.TeamMembership{}, "tmm_notice_rejoined", "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceArchived, entity.ResourceActive},
	} {
		if err := db.Model(mutation.model).Where("id = ?", mutation.id).Update(mutation.field, mutation.value).Error; err != nil {
			t.Fatal(err)
		}
		if inbox := page("caller"); len(inbox.Items) != 0 || inbox.UnreadCount != 0 {
			t.Fatal("inactive subject retained read authority", mutation.id, mutation.value)
		}
		expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+firstMoney.ID+"/read"), 404)
		expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/read-all"), 204)
		if err := db.Model(mutation.model).Where("id = ?", mutation.id).Update(mutation.field, mutation.restore).Error; err != nil {
			t.Fatal(err)
		}
	}
	peerNoticeID := page("peer").Items[0].ID
	if err := db.Model(&entity.User{}).Where("id = ?", users["peer"].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListNotifications(ctx, users["peer"].ID, service.NotificationFilter{}); err == nil {
		t.Fatal("disabled recipient read history")
	}
	if _, err := svc.MarkNotificationRead(ctx, users["peer"].ID, peerNoticeID); err == nil {
		t.Fatal("disabled recipient marked history")
	}
	if err := svc.MarkAllNotificationsRead(ctx, users["peer"].ID); err == nil {
		t.Fatal("disabled recipient marked all history")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users["peer"].ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Exact decimal comparison, stale revision/currency and incomplete coverage.
	writeLimit(teamID, "", `{"tokens_month":null,"money_month":"5.000000000000000001","currency":"USD","reason":"Do not round settled money"}`)
	reconcile()
	if count(teamID) != 2 {
		t.Fatal("money exhaustion rounded up across exact decimal limit")
	}
	writeLimit("tea_notice_uncovered", "", `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Missing journal coverage"}`)
	writeLimit("tea_notice_inactive", "", `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Inactive Team suppression"}`)
	if err := db.Model(&entity.Team{}).Where("id = ?", "tea_notice_inactive").Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	writeLimit("tea_notice_stale", "", `{"tokens_month":10,"reason":"Publish prior policy"}`)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", "tea_notice_stale").Updates(map[string]any{"tokens_month": 0, "ETag": "lim_notice_unpublished"}).Error; err != nil {
		t.Fatal(err)
	}
	reconcileSavedSnapshot()
	for _, suffix := range []string{"uncovered", "inactive", "stale"} {
		if count("tea_notice_"+suffix) != 0 {
			t.Fatal("ineligible current Team produced observation", suffix)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	unknown.Store(true)
	expectStatus(t, native("tea_notice_unknown", false), 200)
	unknown.Store(false)
	truncated.Store(true)
	expectStatus(t, native("tea_notice_truncated", true), 200)
	truncated.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"unknown", "truncated"} {
		team := "tea_notice_" + suffix
		writeLimit(team, "", `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Unknown usage suppresses exhaustion"}`)
		usage := readLimit(team, "").QuotaUsage
		if usage == nil || usage.Month == nil || usage.Month.TokensUnknown == 0 || usage.Month.MoneyUnknown == 0 {
			t.Fatal("missing usage became known zero", suffix, usage)
		}
	}
	reconcile()
	for _, suffix := range []string{"unknown", "truncated"} {
		if count("tea_notice_"+suffix) != 0 {
			t.Fatal("unknown settlement produced observation", suffix)
		}
	}
	// Stored denomination differing from the journal/platform never proves money.
	writeLimit(teamID, "", `{"tokens_month":null,"money_month":"0","currency":"USD","reason":"Observe only a matching authoritative denomination"}`)
	var oldCurrency entity.PricingSetting
	if err := db.First(&oldCurrency, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	reconcileSavedSnapshot()
	if count(teamID) != 2 {
		t.Fatal("currency mismatch manufactured monetary exhaustion")
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", oldCurrency.PlatformCurrency).Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	if count(teamID) != 3 {
		t.Fatal("matching restored denomination did not establish exact money exhaustion")
	}
	writeLimit(teamID, "", `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Independent finite zero generation"}`)
	reconcile()
	if count(teamID) != 5 {
		t.Fatal("finite zero revision was treated as unlimited or replayed prior identity")
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var records int64
		if err := db.Model(model).Where("kind = ?", "monthly_quota_exhausted").Count(&records).Error; err != nil || records != 0 {
			t.Fatal("Team observation borrowed operational/SMTP fanout", records, err)
		}
	}
	var nonTeam int64
	if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_kind NOT IN ?", []string{"team", "team_member"}).Count(&nonTeam).Error; err != nil || nonTeam != 0 {
		t.Fatal("Team extension emitted Personal/Project observations", nonTeam, err)
	}
	assertPrivateHistory := func() {
		t.Helper()
		var rows []entity.QuotaNotificationObservation
		if err := db.Where("scope_kind = ?", "team_member").Find(&rows).Error; err != nil || len(rows) != 2 || len(privateSnapshots) != 2 {
			t.Fatal("canonical independent child observations missing or duplicated", len(rows), len(privateSnapshots), err)
		}
		dimensions := map[string]bool{}
		for _, row := range rows {
			if row.ScopeKind != "team_member" || row.ScopeID != childScope || row.TeamID == nil || *row.TeamID != teamID || row.MemberUserID == nil || *row.MemberUserID != users["caller"].ID || row.PolicyRevision != childRevision || row.ScopeName != "Frozen Team notice name" || row.Limit != "5" || row.Settled != "5" || dimensions[row.Dimension] || (row.Dimension != "tokens" && row.Dimension != "money") || (row.Dimension == "tokens" && row.Currency != "") || (row.Dimension == "money" && row.Currency != "USD") {
				t.Fatal("canonical child observation proof changed", row)
			}
			dimensions[row.Dimension] = true
			var recipients []entity.QuotaNotificationInbox
			if err := db.Where("observation_id = ?", row.ID).Find(&recipients).Error; err != nil || len(recipients) != 1 || recipients[0].ObservationID != row.ID || recipients[0].RecipientID != users["caller"].ID || privateSnapshots[recipients[0].ID] == "" {
				t.Fatal("child notification fanned out beyond original self recipient", recipients, err)
			}
		}
	}
	assertPrivateHistory()
	testTeamQuotaInboxAliases(t, db, svc, teamID, users["caller"].ID, func(method, path string) *httptest.ResponseRecorder { return actorRequest("caller", method, path) })
	// Reopen the same durable journal and use the original persisted Session cookie.
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	beforeRestart := count(teamID)
	reconcile()
	if count(teamID) != beforeRestart {
		t.Fatal("restart duplicated Team observations")
	}
	restored := page("caller")
	for _, record := range restored.Items {
		if record.ID == firstToken.ID && (!record.Read || record.ReadAt == nil || !record.ReadAt.Equal(*read.ReadAt)) {
			t.Fatal("restart reset frozen read receipt")
		}
	}
	if restored.UnreadCount != 0 {
		t.Fatal("read-all state was not durable", restored)
	}
	if usage := readLimit(teamID, users["caller"].ID).QuotaUsage; usage == nil || usage.Month == nil || usage.Month.TokensUsed != 5 {
		t.Fatal("restart changed stable Team accounting", usage)
	}
	// Concurrent reconciliations use the same authoritative single-node journal.
	// A second empty journal is intentionally not an accounting substitute.
	var wait sync.WaitGroup
	failures := make(chan error, 2)
	wait.Go(func() { failures <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
	wait.Go(func() { failures <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count(teamID) != beforeRestart {
		t.Fatal("concurrent observer replay duplicated observations")
	}
	assertPrivateHistory()
}

func testTeamQuotaInboxAliases(t *testing.T, db *gorm.DB, svc *service.Service, teamID, actor string, request func(string, string) *httptest.ResponseRecorder) {
	t.Helper()
	before := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all"), 200)
	now := time.Now().UTC().Truncate(time.Microsecond)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, alias := range []struct{ name, scope, recipient string }{{"scope", strings.ToUpper(teamID), actor}, {"recipient", teamID, strings.ToUpper(actor)}, {"join", teamID, actor}} {
		observation := entity.QuotaNotificationObservation{ID: "qob_team_alias_" + alias.name, ScopeKind: "team", ScopeID: alias.scope, ScopeName: "Inaccessible alias", Dimension: "tokens", PolicyRevision: "rev_team_alias_" + alias.name, MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: now, Limit: "0", Settled: "0", CoverageStart: now, ResourceCreatedAt: now}
		if err := db.Create(&observation).Error; err != nil {
			t.Fatal(err)
		}
		observationID := observation.ID
		if alias.name == "join" {
			observationID = strings.ToUpper(observationID)
		}
		if err := db.Create(&entity.QuotaNotificationInbox{ID: "qni_team_alias_" + alias.name, ObservationID: observationID, RecipientID: alias.recipient, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	after := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all"), 200)
	if len(after.Items) != len(before.Items) || after.UnreadCount != before.UnreadCount {
		t.Fatal("aliased Team/recipient/observation polluted visible history", after)
	}
	for _, name := range []string{"scope", "recipient", "join"} {
		expectStatus(t, request("POST", "/api/v1/notifications/qni_team_alias_"+name+"/read"), 404)
	}
	if _, err := svc.ListNotifications(context.Background(), strings.ToUpper(actor), service.NotificationFilter{}); err == nil {
		t.Fatal("aliased recipient actor authorized")
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		path := "/api/v1/notifications?status=all&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := decodeCatalogResponse[service.NotificationPage](t, request("GET", path), 200)
		if page.UnreadCount != before.UnreadCount {
			t.Fatal("pagination altered scoped unread count")
		}
		for _, record := range page.Items {
			if seen[record.ID] || strings.Contains(record.ID, "alias") {
				t.Fatal("hidden alias consumed/repeated page slot", record)
			}
			seen[record.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(before.Items) {
		t.Fatal("bounded Team inbox omitted visible history")
	}
	expectStatus(t, request("POST", "/api/v1/notifications/read-all"), 204)
	var altered int64
	if err := db.Model(&entity.QuotaNotificationInbox{}).Where("id IN ? AND read_at IS NOT NULL", []string{"qni_team_alias_scope", "qni_team_alias_recipient", "qni_team_alias_join"}).Count(&altered).Error; err != nil || altered != 0 {
		t.Fatal("read-all mutated inaccessible alias", altered, err)
	}
}
