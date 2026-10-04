package service

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func memberKeyTestID(t *testing.T, prefix string) string {
	t.Helper()
	value, err := id.NewPrefixed(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestMemberKeyReviewRequiresPersistentRevision(t *testing.T) {
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	actor := entity.User{ID: memberKeyTestID(t, "usr"), Role: entity.RoleAdmin}
	subject := entity.User{ID: memberKeyTestID(t, "usr"), Role: entity.RoleMember}
	key := entity.APIKey{ID: memberKeyTestID(t, "key"), UserID: subject.ID, LifecycleRevision: memberKeyTestID(t, "kvr"), Status: entity.KeyActive, Name: "Application", UpdatedAt: now}
	models := []string{"mdl_exact"}
	reviewed := memberKeyETag(key, models)
	if err := memberKeyDisableReview(actor, subject, key, models, reviewed, now); err != nil {
		t.Fatal(err)
	}
	first := key.LifecycleRevision
	if err := advancePersonalKeyRevision(&key); err != nil {
		t.Fatal(err)
	}
	// Re-enable after a disable with the SAME timestamp; revision still detects ABA.
	key.Status = entity.KeyActive
	if key.LifecycleRevision == first || key.UpdatedAt != now || memberKeyETag(key, models) == reviewed {
		t.Fatal("revision must distinguish identical-timestamp ABA")
	}
	if !errors.Is(memberKeyDisableReview(actor, subject, key, models, reviewed, now), errKeyConflict) {
		t.Fatal("old review disabled newer active generation")
	}
	key.Status = entity.KeyDisabled
	if err := memberKeyDisableReview(actor, subject, key, models, reviewed, now); err != nil {
		t.Fatalf("current-state disabled retry: %v", err)
	}
	key.LifecycleRevision = ""
	if !errors.Is(memberKeyDisableReview(actor, subject, key, models, reviewed, now), errKeyConflict) {
		t.Fatal("historical missing revision became authoritative")
	}
}

func TestMemberKeyDisableStateAndActorBoundaries(t *testing.T) {
	now := time.Now().UTC()
	actor := entity.User{ID: memberKeyTestID(t, "usr"), Role: entity.RoleMember}
	subject := entity.User{ID: memberKeyTestID(t, "usr"), Role: entity.RoleMember}
	base := entity.APIKey{ID: memberKeyTestID(t, "key"), UserID: subject.ID, LifecycleRevision: memberKeyTestID(t, "kvr"), Status: entity.KeyActive}
	past, future := now.Add(-time.Second), now.Add(time.Second)
	for _, tc := range []struct {
		name           string
		actor, subject entity.User
		key            entity.APIKey
		want           error
	}{
		{"delegated ordinary subject", actor, subject, base, nil},
		{"self", subject, subject, base, apperrors.ErrForbidden},
		{"administrator subject", actor, entity.User{ID: subject.ID, Role: entity.RoleAdmin}, base, apperrors.ErrForbidden},
		{"disabled actor", entity.User{ID: actor.ID, Role: actor.Role, Disabled: true}, subject, base, apperrors.ErrForbidden},
		{"offboarded actor", entity.User{ID: actor.ID, Role: actor.Role, OffboardedAt: &past}, subject, base, apperrors.ErrForbidden},
		{"disabled subject", actor, entity.User{ID: subject.ID, Role: subject.Role, Disabled: true}, base, errKeyConflict},
		{"offboarded subject", actor, entity.User{ID: subject.ID, Role: subject.Role, OffboardedAt: &past}, base, errKeyConflict},
		{"other owner", actor, subject, entity.APIKey{UserID: actor.ID}, apperrors.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := memberKeyDisableReview(tc.actor, tc.subject, tc.key, nil, memberKeyETag(tc.key, nil), now)
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, status := range []string{entity.KeyPending, entity.KeyRevoked, "unknown"} {
		key := base
		key.Status = status
		if !errors.Is(memberKeyDisableReview(actor, subject, key, nil, memberKeyETag(key, nil), now), errKeyConflict) {
			t.Errorf("status %s accepted", status)
		}
	}
	for _, expires := range []*time.Time{&past, &now, &future} {
		key := base
		key.ExpiresAt = expires
		allowed := memberKeyDisableAllowed(actor, subject, key, now)
		if allowed != now.Before(*expires) {
			t.Fatal("expiry boundary")
		}
	}
	if !slices.Contains(AvailablePermissions, "members.keys.disable") || !slices.Contains(AssignablePermissions(), "members.keys.disable") {
		t.Fatal("missing independent explicit permission")
	}
}

func TestMemberKeyPublishedDisabledProofIsExact(t *testing.T) {
	now := time.Now().UTC()
	key := entity.APIKey{ID: memberKeyTestID(t, "key"), UserID: memberKeyTestID(t, "usr"), LifecycleRevision: memberKeyTestID(t, "kvr"), Status: entity.KeyDisabled}
	auth := &runtimeAuthorization{ValidUntil: now.Add(time.Minute), PersonalKeyStates: runtimeMemberKeyStates([]entity.APIKey{key})}
	if !publishedMemberKeyDisabled(auth, key, now) {
		t.Fatal("genuine disabled proof rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*runtimeAuthorization)
	}{
		{"expired lease", func(a *runtimeAuthorization) { a.ValidUntil = now }},
		{"absence", func(a *runtimeAuthorization) { delete(a.PersonalKeyStates, key.ID) }},
		{"owner", func(a *runtimeAuthorization) {
			p := a.PersonalKeyStates[key.ID]
			p.UserID = strings.ToUpper(key.UserID)
			a.PersonalKeyStates[key.ID] = p
		}},
		{"revision", func(a *runtimeAuthorization) {
			p := a.PersonalKeyStates[key.ID]
			p.Revision = memberKeyTestID(t, "kvr")
			a.PersonalKeyStates[key.ID] = p
		}},
		{"active", func(a *runtimeAuthorization) {
			p := a.PersonalKeyStates[key.ID]
			p.Status = entity.KeyActive
			a.PersonalKeyStates[key.ID] = p
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &runtimeAuthorization{ValidUntil: now.Add(time.Minute), PersonalKeyStates: runtimeMemberKeyStates([]entity.APIKey{key})}
			tc.mutate(a)
			if publishedMemberKeyDisabled(a, key, now) {
				t.Fatal("false publication proof")
			}
		})
	}
	for _, bad := range []entity.APIKey{{ID: key.ID, UserID: key.UserID, Status: entity.KeyDisabled}, {ID: key.ID, UserID: strings.ToUpper(key.UserID), LifecycleRevision: key.LifecycleRevision, Status: key.Status}} {
		if len(runtimeMemberKeyStates([]entity.APIKey{bad})) != 0 {
			t.Fatal("invalid canonical proof")
		}
	}
	alias := key
	alias.ID = strings.ToUpper(key.ID)
	for _, rows := range [][]entity.APIKey{{key, key}, {key, alias}} {
		if len(runtimeMemberKeyStates(rows)) != 0 {
			t.Fatal("ambiguous duplicate/collation alias proof")
		}
	}
	if publishedMemberKeyDisabled(nil, key, now) {
		t.Fatal("nil publication")
	}
}

func TestMemberKeyExactCounterProjectionAndNoSecrets(t *testing.T) {
	large := int64(math.MaxInt64)
	value, err := memberKeyQuotaWindow(&QuotaWindowRecord{Covered: false, TokensUsed: large, TokensHeld: 9007199254740993, TokensUnknown: large, MoneyUnknown: large, MoneyUsed: map[string]string{"USD": "9007199254740993.000000000000000001"}, MoneyHeld: map[string]string{"EUR": "0.000000000000000001"}})
	if err != nil || value.TokensUsed != "9223372036854775807" || value.TokensHeld != "9007199254740993" || value.Covered || value.MoneyUsed["USD"] != "9007199254740993.000000000000000001" {
		t.Fatalf("lossy quota: %+v %v", value, err)
	}
	if converted, err := memberKeyCounter(&large); err != nil || *converted != "9223372036854775807" {
		t.Fatal("lossy live counter")
	}
	for _, negative := range []*QuotaWindowRecord{{TokensUsed: -1}, {TokensHeld: -1}, {TokensUnknown: -1}, {MoneyUnknown: -1}} {
		if _, err := memberKeyQuotaWindow(negative); err == nil {
			t.Fatal("negative historical counter accepted")
		}
	}
	if value, err := memberKeyCounter(nil); err != nil || value != nil {
		t.Fatal("invented unknown live counter")
	}
	row := MemberKeyRecord{ID: memberKeyTestID(t, "key"), Name: "Application", Status: entity.KeyDisabled, ModelIDs: []string{"mdl_retained"}, LastUseCoverage: "unknown", Limits: &MemberKeyLimitRecord{QuotaUsage: &MemberKeyQuotaUsage{Month: value}}}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret", "prefix", "token_hash", "lifecycle_revision", "delivery_expires_at"} {
		if _, exists := fields[forbidden]; exists {
			t.Fatal("unsafe DTO field " + forbidden)
		}
	}
	if !strings.Contains(string(raw), `"tokens_used":"9223372036854775807"`) {
		t.Fatal("unsafe JSON counter")
	}
}

func TestMemberKeyLastUseKeepsExactPersonalAttribution(t *testing.T) {
	owner := memberKeyTestID(t, "usr")
	key := memberKeyTestID(t, "key")
	now := time.Now().UTC()
	facts := []entity.CallRecord{{UserID: owner, KeyID: key, StartedAt: now}, {UserID: strings.ToUpper(owner), KeyID: key, StartedAt: now.Add(time.Hour)}, {UserID: owner, KeyID: strings.ToUpper(key), StartedAt: now.Add(time.Hour)}, {UserID: owner, KeyID: key, ProjectID: "prj_other", StartedAt: now.Add(time.Hour)}, {UserID: owner, KeyID: key}}
	if got := memberKeyLastUseFacts(facts, owner, []string{key}); !reflect.DeepEqual(got, map[string]time.Time{key: now}) {
		t.Fatalf("wrong attribution: %+v", got)
	}
}
