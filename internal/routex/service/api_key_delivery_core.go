package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var apiKeyDeliveryUnavailable = &apperrors.Error{Code: 503, Message: "API-Key delivery unavailable"}
var apiKeyDeliveryConflict = &apperrors.Error{Code: 409, Message: "API-Key delivery state changed"}

func apiKeyDeliveryError(e error) error {
	if e == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(e, &app) {
		return app
	}
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return apiKeyDeliveryUnavailable
}
func apiKeyDeliveryDigest(domain string, v any) string {
	raw, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	return secret.SHA256Hex(domain + "\n" + string(raw))
}
func apiKeyDeliveryRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", apiKeyDeliveryUnavailable
	}
	return secret.SHA256Hex(v), nil
}
func apiKeyDeliveryContext(ctx context.Context, read bool) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, func() {}, apperrors.ErrBadRequest
	}
	d := 10 * time.Second
	if read {
		d = 3 * time.Second
	}
	c, cancel := context.WithTimeout(ctx, d)
	if e := c.Err(); e != nil {
		cancel()
		return nil, func() {}, apiKeyDeliveryUnavailable
	}
	return c, cancel, nil
}
func apiKeyDeliveryText(v string, max int) bool {
	return v != "" && len(v) <= max && utf8.ValidString(v) && !strings.ContainsFunc(v, unicode.IsControl)
}
func apiKeyDeliveryCode(v string) bool {
	if len(v) < 1 || len(v) > 64 {
		return false
	}
	for _, c := range []byte(v) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
func apiKeyDeliveryBirth(t time.Time) bool {
	return !t.IsZero() && t.Equal(t.Truncate(time.Microsecond))
}
func apiKeyDeliveryOwnerDigest(kind, id string, birth time.Time) string {
	return apiKeyDeliveryDigest("api-key-delivery:owner:v1", struct {
		Kind, ID string
		Birth    time.Time
	}{kind, id, birth.UTC()})
}
func apiKeyDeliveryOwnerCurrent(tx *gorm.DB, a *Authentication, project string) (apiKeyDeliveryOwner, error) {
	if a == nil {
		return apiKeyDeliveryOwner{}, apperrors.ErrUnauthorized
	}
	if e := lockGovernance(tx); e != nil {
		return apiKeyDeliveryOwner{}, e
	}
	u, e := registrationAdmittedUser(tx, a.User.ID, true)
	if e != nil {
		return apiKeyDeliveryOwner{}, e
	}
	if e = identityCurrentSession(tx, a, u, true); e != nil {
		return apiKeyDeliveryOwner{}, e
	}
	o := apiKeyDeliveryOwner{Kind: "personal", ID: u.ID, Birth: u.CreatedAt}
	if project != "" {
		p, e := lockProjectForKeys(tx, u.ID, project, true)
		if e != nil {
			return o, e
		}
		if p.ID != project {
			return o, apperrors.ErrNotFound
		}
		o.Kind, o.ID, o.Birth = "project", p.ID, p.CreatedAt
	}
	if !apiKeyDeliveryBirth(o.Birth) {
		return o, apiKeyDeliveryUnavailable
	}
	o.Digest = apiKeyDeliveryOwnerDigest(o.Kind, o.ID, o.Birth)
	return o, nil
}
func apiKeyDeliveryAdmin(tx *gorm.DB, a *Authentication, permission string, test bool) (entity.User, error) {
	if a == nil {
		return entity.User{}, apperrors.ErrUnauthorized
	}
	if e := lockGovernance(tx); e != nil {
		return entity.User{}, e
	}
	u, e := vaultAuthorize(tx, a.User.ID, permission)
	if e != nil {
		return u, e
	}
	if e = identityCurrentSession(tx, a, u, true); e != nil {
		return u, e
	}
	if test {
		_, e = vaultAuthorize(tx, a.User.ID, "secrets.test")
	}
	return u, e
}
func apiKeyDeliveryPolicy(tx *gorm.DB, lock bool) (entity.APIKeyDeliveryPolicy, error) {
	var p entity.APIKeyDeliveryPolicy
	q := vaultDB(tx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Take(&p, 1).Error
	if e != nil {
		return p, e
	}
	if p.ID != 1 || !personalModelETag(p.ReviewRevision) || !personalModelETag(p.SecurityRevision) || (!p.AllowManual && !p.AllowManagedVault) || p.DefaultMode != "manual" && p.DefaultMode != "managed_vault" || p.DefaultMode == "manual" && !p.AllowManual || p.DefaultMode == "managed_vault" && !p.AllowManagedVault {
		return p, apiKeyDeliveryUnavailable
	}
	return p, nil
}
func apiKeyDeliveryPolicyReview(p entity.APIKeyDeliveryPolicy, a *Authentication) string {
	return apiKeyDeliveryDigest("api-key-delivery:policy-review:v1", struct {
		Review, Actor, Session   string
		ActorBirth, SessionBirth time.Time
	}{p.ReviewRevision, a.User.ID, a.Session.ID, a.User.CreatedAt.UTC(), a.Session.CreatedAt.UTC()})
}
func apiKeyDeliveryOwnerReview(p entity.APIKeyDeliveryPolicy, a *Authentication, o apiKeyDeliveryOwner) string {
	return apiKeyDeliveryDigest("api-key-delivery:owner-review:v1", struct{ Policy, Owner string }{apiKeyDeliveryPolicyReview(p, a), o.Digest})
}
func (s *Service) apiKeyDeliveryInfrastructure(rev entity.VaultRevision) string {
	return apiKeyDeliveryDigest("api-key-delivery:infrastructure:v1", struct {
		ID, Integration         string
		Birth, IntegrationBirth time.Time
		Descriptor              VaultDescriptor
		AllowPrivate            bool
	}{rev.ID, rev.IntegrationID, rev.CreatedAt.UTC(), rev.IntegrationBirth.UTC(), vaultDescriptor(rev), s.allowPrivateUpstream})
}
func (s *Service) apiKeyDeliverySnapshot(tx *gorm.DB, p entity.APIKeyDeliveryPolicy, requireVerified bool) (apiKeyDeliverySnapshot, error) {
	x := apiKeyDeliverySnapshot{Policy: p}
	if p.IntegrationBirth == nil || p.VaultRevisionBirth == nil || p.WriterIdentityID == p.ProvisionerIdentityID || p.TemplateID != "jwt" {
		return x, apiKeyDeliveryUnavailable
	}
	if e := personalExact(vaultDB(tx), "id", p.TemplateID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&x.Template).Error; e != nil {
		return x, e
	}
	if x.Template.ID != p.TemplateID || x.Template.SecurityRevision != p.TemplateSecurityRevision {
		return x, apiKeyDeliveryConflict
	}
	if e := personalExact(vaultDB(tx), "id", p.IntegrationID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&x.Integration).Error; e != nil {
		return x, e
	}
	if x.Integration.ID != p.IntegrationID || !x.Integration.CreatedAt.Equal(*p.IntegrationBirth) || x.Integration.RevisionID != p.VaultRevisionID {
		return x, apiKeyDeliveryConflict
	}
	if e := personalExact(vaultDB(tx), "id", p.VaultRevisionID).Take(&x.Revision).Error; e != nil {
		return x, e
	}
	if x.Revision.ID != p.VaultRevisionID || !x.Revision.CreatedAt.Equal(*p.VaultRevisionBirth) || x.Revision.IntegrationID != p.IntegrationID || !x.Revision.IntegrationBirth.Equal(*p.IntegrationBirth) || s.apiKeyDeliveryInfrastructure(x.Revision) != p.IntegrationRevision || p.InfrastructureSecurityRevision != p.IntegrationRevision {
		return x, apiKeyDeliveryConflict
	}
	for i, target := range []string{p.WriterIdentityID, p.ProvisionerIdentityID} {
		dst := &x.Writer
		purpose := "writer"
		if i == 1 {
			dst = &x.Provisioner
			purpose = "provisioner"
		}
		if e := personalExact(vaultDB(tx), "id", target).Clauses(clause.Locking{Strength: "UPDATE"}).Take(dst).Error; e != nil {
			return x, e
		}
		if dst.ID != target || dst.Purpose != purpose || dst.Method != "token" || dst.IntegrationID != p.IntegrationID || !dst.IntegrationBirth.Equal(*p.IntegrationBirth) || dst.IntegrationRevision != p.IntegrationRevision || dst.VaultRevisionID != p.VaultRevisionID || !dst.VaultRevisionBirth.Equal(*p.VaultRevisionBirth) || dst.AuthCiphertext == "" {
			return x, apiKeyDeliveryConflict
		}
		if _, e := apiKeyDeliveryAuthReference(*dst); e != nil {
			return x, e
		}
	}
	if e := apiKeyDeliveryInfrastructureVerified(tx, x.Integration, x.Revision); e != nil {
		return x, e
	}
	if requireVerified && (p.VerifiedRevision != p.SecurityRevision || p.VerifiedAt == nil || x.Template.VerifiedRevision != x.Template.SecurityRevision || x.Template.VerifiedAt == nil) {
		return x, apiKeyDeliveryUnavailable
	}
	return x, nil
}
func apiKeyDeliveryManualAllowed(tx *gorm.DB) error {
	p, e := apiKeyDeliveryPolicy(tx, true)
	if e != nil {
		return e
	}
	if !p.AllowManual {
		return apiKeyDeliveryConflict
	}
	return nil
}
func apiKeyDeliveryProfile(tx *gorm.DB, target string) (entity.VaultAccessProfile, entity.VaultPrincipalBinding, error) {
	var p entity.VaultAccessProfile
	var b entity.VaultPrincipalBinding
	if !rootSafeIdentity(target, 30) {
		return p, b, apperrors.ErrBadRequest
	}
	e := personalExact(vaultDB(tx), "id", target).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&p).Error
	if e != nil {
		return p, b, e
	}
	if p.ID != target || !apiKeyDeliveryBirth(p.CreatedAt) {
		return p, b, apiKeyDeliveryUnavailable
	}
	e = personalExact(vaultDB(tx), "profile_id", p.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&b).Error
	if e == nil && (b.ProfileID != p.ID || !b.ProfileBirth.Equal(p.CreatedAt) || b.Method != "jwt" || !apiKeyDeliveryBirth(b.CreatedAt)) {
		e = apiKeyDeliveryUnavailable
	}
	return p, b, e
}
func apiKeyDeliveryTargetReview(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, a *Authentication) string {
	return apiKeyDeliveryDigest("api-key-delivery:target-review:v1", struct {
		ID, Review, Config, Security, Principal, PrincipalSecurity, Approval, Isolation, IsolationState, Actor, Session string
		ActorBirth, SessionBirth                                                                                        time.Time
	}{p.ID, p.ReviewRevision, p.ConfigRevision, p.SecurityRevision, b.ID, b.SecurityRevision, b.ApprovalRevision, p.CurrentIsolationRevision, p.IsolationState, a.User.ID, a.Session.ID, a.User.CreatedAt.UTC(), a.Session.CreatedAt.UTC()})
}
func apiKeyDeliveryOwnerMatches(p entity.VaultAccessProfile, o apiKeyDeliveryOwner) bool {
	return p.OwnerKind == o.Kind && p.OwnerID == o.ID && p.OwnerBirth.Equal(o.Birth) && p.OwnerDigest == o.Digest
}
func apiKeyDeliverySubjectAllowed(t entity.APIKeyCallerTemplate, subject string) bool {
	var values []string
	if len(t.AllowedSubjectsJSON) > 32768 || json.Unmarshal([]byte(t.AllowedSubjectsJSON), &values) != nil || len(values) == 0 || len(values) > 128 {
		return false
	}
	return slices.Contains(values, subject)
}
func apiKeyDeliveryFailure(code string) *string {
	if code == "" {
		return nil
	}
	return &code
}
func apiKeyDeliveryAudit(tx *gorm.DB, actor, action, target, request, reason string) error {
	if !validRegistrationReason(reason) {
		return apperrors.ErrBadRequest
	}
	// Only this closed nonsecret detail is retained. Central audit projection is a
	// separately owned HTTP/UI compatibility seam; no arbitrary JSON is returned.
	detail := struct {
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
	}{request, reason}
	raw, e := json.Marshal(detail)
	if e != nil {
		return e
	}
	id, e := newAPIKeyDeliveryID("aud")
	if e != nil {
		return e
	}
	text := string(raw)
	return tx.Create(&entity.AuditEvent{ID: id, ActorID: actor, Action: "api_key.delivery." + action, ResourceType: "api_key_delivery", ResourceID: target, DetailsJSON: &text}).Error
}

func apiKeyDeliveryInfrastructureVerified(tx *gorm.DB, integration entity.VaultIntegration, revision entity.VaultRevision) error {
	var p entity.VaultProbe
	if e := personalExact(vaultDB(tx), "integration_id", integration.ID).Where("revision_id = ? AND state = ?", revision.ID, "completed").Order("created_at DESC").Order("id DESC").Take(&p).Error; e != nil {
		return apiKeyDeliveryUnavailable
	}
	if p.IntegrationID != integration.ID || !p.IntegrationBirth.Equal(integration.CreatedAt) || p.RevisionID != revision.ID || p.State != "completed" || p.Version != 1 || p.FinishedAt == nil {
		return apiKeyDeliveryUnavailable
	}
	var write, read VaultObservationView
	var cleanup VaultCleanupView
	if !vaultDecodeObservation(p.WriteJSON, &write) || !vaultDecodeObservation(p.ReadJSON, &read) || !vaultDecodeCleanup(p.CleanupJSON, &cleanup) || !write.Attempted || !write.Succeeded || write.Failure != nil || !read.Attempted || !read.Succeeded || read.Failure != nil || cleanup.State != "acknowledged" || !cleanup.Observation.Attempted || !cleanup.Observation.Succeeded || cleanup.Observation.Failure != nil {
		return apiKeyDeliveryUnavailable
	}
	return nil
}
