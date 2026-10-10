package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"sort"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

const (
	runtimeProjectionMaxBytes   = 16 << 20
	runtimeProjectionMaxEntries = 262144
)

var errRuntimeAuthorizationProjection = errors.New("runtime authorization projection unavailable")

// runtimeAuthorizationProjection describes the actual immutable installed source,
// not desired database state or live tombstones. The caller owns the one original
// evidence deadline. Failure supplies no partial digest and never changes admission.
func runtimeAuthorizationProjection(ctx context.Context, auth *runtimeAuthorization) (string, error) {
	if ctx == nil || auth == nil {
		return "", errRuntimeAuthorizationProjection
	}
	w := &runtimeProjectionWriter{ctx: ctx, digest: sha256.New()}
	w.text("routex.gateway-authorization.v1")
	// publicationEpoch and ValidUntil are current guards, not logical source.
	// installationProjectionDigest is cache metadata; never recursively hash it.
	w.field("Connections")
	runtimeProjectionMap(w, auth.Connections, func(key string, v runtimeConnectionProof) {
		w.text(v.ProviderID)
		w.instant(v.Birth)
		w.boolean(v.Enabled)
		w.text(v.TransportGeneration)
	})
	w.field("Providers")
	runtimeProjectionMap(w, auth.Providers, func(key string, v runtimeProviderProof) { w.instant(v.Birth); w.boolean(v.Enabled); w.text(v.Revision) })
	w.field("PersonalGrantStates")
	runtimeProjectionMap(w, auth.PersonalGrantStates, func(key string, v runtimePersonalGrantState) {
		w.instant(v.CreatedAt)
		w.boolean(v.Enabled)
		w.text(v.Revision)
		w.text(v.Hash)
	})
	w.field("ModelEligibilityHashes")
	runtimeProjectionMap(w, auth.ModelEligibilityHashes, func(key string, v string) { w.text(v) })
	w.field("PersonalKeyStates")
	runtimeProjectionMap(w, auth.PersonalKeyStates, func(key string, v runtimePersonalKeyState) { w.text(v.UserID); w.text(v.Revision); w.text(v.Status) })
	w.field("UserAdmissions")
	runtimeProjectionMap(w, auth.UserAdmissions, func(key string, v runtimeAdmissionProof) {
		w.instant(v.CreatedAt)
		w.text(v.ApplicationID)
		w.instant(v.ApplicationCreatedAt)
		w.text(v.Revision)
		w.text(v.State)
		w.boolean(v.Eligible)
	})
	w.field("UserProofs")
	runtimeProjectionMap(w, auth.UserProofs, func(key string, v runtimeUserProof) { w.instant(v.CreatedAt); w.boolean(v.Enabled) })
	w.field("ProjectCreationStates")
	runtimeProjectionMap(w, auth.ProjectCreationStates, func(key string, v runtimeProjectCreationState) {
		if key != v.Project.ID || key == "" {
			w.fail()
			return
		}
		w.project(v.Project)
		runtimeProjectionMap(w, v.Managers, func(_ string, value string) { w.text(value) })
		runtimeProjectionMap(w, v.EnabledManagers, func(_ string, value bool) { w.boolean(value) })
		w.set(v.Models)
		w.boolean(v.Eligible)
	})
	w.field("PersonalGrantSources")
	runtimeProjectionMap(w, auth.PersonalGrantSources, func(key string, v map[string]string) {
		runtimeProjectionMap(w, v, func(_ string, value string) { w.text(value) })
	})
	w.field("TeamGrantSources")
	runtimeProjectionMap(w, auth.TeamGrantSources, func(key string, v map[string]string) {
		runtimeProjectionMap(w, v, func(_ string, value string) { w.text(value) })
	})
	w.field("TeamCreationGrants")
	runtimeProjectionMap(w, auth.TeamCreationGrants, func(key string, v map[string]runtimeTeamCreationGrant) {
		runtimeProjectionMap(w, v, func(_ string, value runtimeTeamCreationGrant) {
			w.instant(value.ModelCreatedAt)
			w.text(value.CreationID)
			w.text(value.RequestID)
		})
	})
	w.field("SourceDigest")
	w.text(auth.SourceDigest)
	w.field("CredentialRevisions")
	runtimeProjectionMap(w, auth.CredentialRevisions, func(key string, v string) { w.text(v) })
	w.field("ProviderModelRevisions")
	runtimeProjectionMap(w, auth.ProviderModelRevisions, func(key string, v string) { w.text(v) })
	w.field("Quota")
	w.quota(auth.Quota)
	w.field("ConnectionRevisions")
	runtimeProjectionMap(w, auth.ConnectionRevisions, func(key string, v string) { w.text(v) })
	w.field("LimitPolicies")
	runtimeProjectionMap(w, auth.LimitPolicies, func(key string, v limits.Policy) { w.policy(v) })
	w.field("LimitRoots")
	runtimeProjectionMap(w, auth.LimitRoots, func(key string, v string) { w.text(v) })
	w.field("PersonalLimitOwners")
	runtimeProjectionMap(w, auth.PersonalLimitOwners, func(key string, v string) { w.text(v) })
	w.field("ProjectLimitOwners")
	runtimeProjectionMap(w, auth.ProjectLimitOwners, func(key string, v projectKeyMonthlyIdentity) {
		if key == "" || key != v.Root.ID || v.Root.ProjectID == "" || v.Root.ProjectID != v.Project.ID || v.Root.ReplacesKeyID != nil {
			w.fail()
			return
		}
		w.projectRoot(v.Root)
		w.project(v.Project)
	})
	w.field("Keys")
	w.keys(auth)
	w.field("KeysByID")
	// Exact derived bijection is checked by keys; nilness is still typed source.
	w.boolean(auth.KeysByID == nil)
	w.field("Names")
	runtimeProjectionMap(w, auth.Names, func(key string, v entity.ModelName) {
		if key == "" || key != v.Name {
			w.fail()
			return
		}
		w.text(v.Name)
		w.text(v.ModelID)
		w.optionalText(v.CurrentModelID)
		w.optionalTime(v.ExpiresAt)
		w.instant(v.CreatedAt)
	})
	w.field("Models")
	runtimeProjectionMap(w, auth.Models, func(key string, v bool) { w.boolean(v) })
	w.field("Credentials")
	runtimeProjectionMap(w, auth.Credentials, func(key string, v bool) { w.boolean(v) })
	w.field("ProviderModels")
	runtimeProjectionMap(w, auth.ProviderModels, func(key string, v bool) { w.boolean(v) })
	w.field("CredentialAccess")
	runtimeProjectionMap(w, auth.CredentialAccess, func(key string, v map[string]bool) {
		runtimeProjectionMap(w, v, func(_ string, value bool) { w.boolean(value) })
	})
	w.field("ModelCreated")
	runtimeProjectionMap(w, auth.ModelCreated, func(key string, v time.Time) { w.instant(v) })
	w.field("TeamSessions")
	w.sessions(auth.TeamSessions)
	w.field("Teams")
	runtimeProjectionMap(w, auth.Teams, func(key string, v runtimeTeam) {
		w.instant(v.CreatedAt)
		runtimeProjectionMap(w, v.Members, func(_ string, value string) { w.text(value) })
		runtimeProjectionMap(w, v.Models, func(_ string, value bool) { w.boolean(value) })
	})
	if !w.check() {
		return "", errRuntimeAuthorizationProjection
	}
	return hex.EncodeToString(w.digest.Sum(nil)), nil
}

// Each value is a one-byte type and an unsigned 64-bit length followed by exact
// bytes. Map/set counts and nil markers prevent concatenation or null aliases.
// Hash writes are chunked; no serialized authorization document is retained.
type runtimeProjectionWriter struct {
	ctx     context.Context
	digest  hash.Hash
	bytes   int
	entries int
	failed  bool
}

func (w *runtimeProjectionWriter) fail() { w.failed = true }
func (w *runtimeProjectionWriter) check() bool {
	if w.failed || w.ctx.Err() != nil {
		w.failed = true
		return false
	}
	return true
}
func (w *runtimeProjectionWriter) visit(n int) bool {
	if !w.check() || n < 0 || n > runtimeProjectionMaxEntries-w.entries {
		w.fail()
		return false
	}
	w.entries += n
	return true
}
func (w *runtimeProjectionWriter) frame(kind byte, value string) {
	if !w.visit(1) || len(value) > runtimeProjectionMaxBytes-w.bytes-9 {
		w.fail()
		return
	}
	var header [9]byte
	header[0] = kind
	binary.BigEndian.PutUint64(header[1:], uint64(len(value)))
	_, _ = w.digest.Write(header[:])
	w.bytes += 9 + len(value)
	for len(value) > 0 {
		if !w.check() {
			return
		}
		n := min(len(value), 4096)
		_, _ = w.digest.Write([]byte(value[:n]))
		value = value[n:]
	}
}
func (w *runtimeProjectionWriter) field(v string) { w.frame('f', v) }
func (w *runtimeProjectionWriter) text(v string)  { w.frame('s', v) }
func (w *runtimeProjectionWriter) boolean(v bool) {
	if v {
		w.frame('b', "1")
	} else {
		w.frame('b', "0")
	}
}
func (w *runtimeProjectionWriter) integer(v int64) { w.frame('i', strconv.FormatInt(v, 10)) }
func (w *runtimeProjectionWriter) instant(v time.Time) {
	v = v.UTC()
	if v.Year() < 0 || v.Year() > 9999 {
		w.fail()
		return
	}
	w.frame('t', v.Format(time.RFC3339Nano))
}
func (w *runtimeProjectionWriter) optionalText(v *string) {
	if v == nil {
		w.frame('n', "string")
		return
	}
	w.frame('p', "string")
	w.text(*v)
}
func (w *runtimeProjectionWriter) optionalTime(v *time.Time) {
	if v == nil {
		w.frame('n', "time")
		return
	}
	w.frame('p', "time")
	w.instant(*v)
}
func (w *runtimeProjectionWriter) optionalInteger(v *int64) {
	if v == nil {
		w.frame('n', "integer")
		return
	}
	w.frame('p', "integer")
	w.integer(*v)
}
func (w *runtimeProjectionWriter) sortedSet(values []string) []string {
	if !w.visit(len(values)) {
		return nil
	}
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	if !w.check() {
		return nil
	}
	for i := 1; i < len(copyValues); i++ {
		if copyValues[i] == copyValues[i-1] {
			w.fail()
			return nil
		}
	}
	return copyValues
}
func (w *runtimeProjectionWriter) set(values []string) {
	if values == nil {
		w.frame('n', "set")
		return
	}
	w.frame('S', strconv.Itoa(len(values)))
	for _, value := range w.sortedSet(values) {
		w.text(value)
	}
}

// Generic traversal is typed at every closed callsite; there is no arbitrary
// value admission, reflection fallback, exported writer or entity JSON encoding.
func runtimeProjectionMap[V any](w *runtimeProjectionWriter, values map[string]V, encode func(string, V)) {
	if values == nil {
		w.frame('n', "map")
		return
	}
	if !w.visit(len(values)) {
		return
	}
	w.frame('M', strconv.Itoa(len(values)))
	keys := make([]string, 0, len(values))
	for key := range values {
		if !w.check() {
			return
		}
		if key == "" {
			w.fail()
			return
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !w.check() {
		return
	}
	for _, key := range keys {
		if !w.check() {
			return
		}
		w.text(key)
		encode(key, values[key])
	}
}

func (w *runtimeProjectionWriter) project(v entity.Project) {
	w.text(v.ID)
	w.text(v.Status)
	w.text(v.CreatorID)
	w.instant(v.CreatedAt)
}
func (w *runtimeProjectionWriter) projectRoot(v entity.ProjectKey) {
	if v.ReplacesKeyID != nil {
		w.fail()
		return
	}
	w.text(v.ID)
	w.text(v.ProjectID)
	w.text(v.CreatorID)
	w.text(v.Status)
	w.optionalTime(v.ExpiresAt)
	w.instant(v.CreatedAt)
	w.text(w.verifier("project-key", v.TokenHash))
	w.frame('n', "root-replaces-key-id")
}
func (w *runtimeProjectionWriter) quota(v *runtimeQuotaData) {
	if v == nil {
		w.frame('n', "quota")
		return
	}
	w.frame('p', "quota")
	w.integer(int64(v.Setting.ID))
	w.boolean(v.Setting.AccountingStarted)
	w.text(v.Setting.TimeZone)
	w.text(v.Setting.ETag)
	runtimeProjectionMap(w, v.Bounds, func(key string, bound entity.ReservationBound) {
		if key == "" || key != bound.ProviderModelID {
			w.fail()
			return
		}
		w.text(bound.ProviderModelID)
		w.text(bound.Protocol)
		w.text(bound.TransportGeneration)
		w.integer(bound.MaxInputTokens)
		w.integer(bound.MaxOutputTokens)
		w.text(bound.ETag)
	})
	runtimeProjectionMap(w, v.Created, func(_ string, value time.Time) { w.instant(value) })
	runtimeProjectionMap(w, v.Revisions, func(_ string, value string) { w.text(value) })
	w.text(v.Currency)
}
func (w *runtimeProjectionWriter) policy(v limits.Policy) {
	w.optionalInteger(v.Tokens5H)
	w.optionalInteger(v.Tokens7D)
	w.text(v.TokensMonthBehavior)
	w.text(v.MoneyMonthBehavior)
	w.optionalInteger(v.TokensMonth)
	w.optionalInteger(v.TPM)
	w.optionalText(v.MoneyMonth)
	w.text(v.Currency)
	w.optionalInteger(v.RPM)
	w.optionalInteger(v.Concurrency)
	w.text(v.IPMode)
	w.set(v.IPRanges)
}

func (w *runtimeProjectionWriter) verifier(purpose, digest string) string {
	if len(digest) != 64 {
		w.fail()
		return ""
	}
	for _, c := range digest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			w.fail()
			return ""
		}
	}
	h := sha256.New()
	for _, value := range []string{"routex.gateway-authorization.verifier.v1", purpose, digest} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func runtimeProjectionKeyPurpose(v runtimeKey) string {
	if v.ProjectID != "" && v.Key.UserID == "" {
		return "project-key"
	}
	if v.ProjectID == "" && v.Key.UserID != "" {
		return "personal-key"
	}
	return ""
}
func (w *runtimeProjectionWriter) key(v runtimeKey) {
	w.text(v.Key.ID)
	w.text(v.Key.UserID)
	w.text(v.ProjectID)
	w.text(v.Key.LifecycleRevision)
	w.text(v.Key.Status)
	w.optionalTime(v.Key.ExpiresAt)
	w.instant(v.Key.CreatedAt)
	w.set(v.Models)
}
func runtimeProjectionSameOptionalTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
func (w *runtimeProjectionWriter) sameKey(a, b runtimeKey) bool {
	if a.Key.ID != b.Key.ID || a.Key.UserID != b.Key.UserID || a.ProjectID != b.ProjectID || a.Key.TokenHash != b.Key.TokenHash || a.Key.LifecycleRevision != b.Key.LifecycleRevision || a.Key.Status != b.Key.Status || !runtimeProjectionSameOptionalTime(a.Key.ExpiresAt, b.Key.ExpiresAt) || !a.Key.CreatedAt.Equal(b.Key.CreatedAt) || (a.Models == nil) != (b.Models == nil) || len(a.Models) != len(b.Models) {
		return false
	}
	left, right := w.sortedSet(a.Models), w.sortedSet(b.Models)
	if !w.check() {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func (w *runtimeProjectionWriter) keys(auth *runtimeAuthorization) {
	if len(auth.Keys) != len(auth.KeysByID) || !w.visit(len(auth.Keys)) {
		w.fail()
		return
	}
	if auth.Keys == nil {
		w.frame('n', "map")
		return
	}
	committed := make(map[string]runtimeKey, len(auth.Keys))
	ids := make(map[string]bool, len(auth.Keys))
	for lookup, value := range auth.Keys {
		if !w.check() {
			return
		}
		derived, exists := auth.KeysByID[value.Key.ID]
		purpose := runtimeProjectionKeyPurpose(value)
		if !exists || value.Key.ID == "" || lookup != value.Key.TokenHash || ids[value.Key.ID] || purpose == "" || !w.sameKey(value, derived) {
			w.fail()
			return
		}
		ids[value.Key.ID] = true
		commitment := w.verifier(purpose, lookup)
		if _, duplicate := committed[commitment]; duplicate {
			w.fail()
			return
		}
		committed[commitment] = value
	}
	runtimeProjectionMap(w, committed, func(_ string, value runtimeKey) { w.key(value) })
}
func (w *runtimeProjectionWriter) sessions(values map[string]runtimeTeamSession) {
	if values == nil {
		w.frame('n', "map")
		return
	}
	if !w.visit(len(values)) {
		return
	}
	committed := make(map[string]runtimeTeamSession, len(values))
	ids := make(map[string]bool, len(values))
	for lookup, value := range values {
		if !w.check() {
			return
		}
		if lookup != value.TokenHash || value.ID == "" || value.UserID == "" || ids[value.ID] {
			w.fail()
			return
		}
		ids[value.ID] = true
		commitment := w.verifier("team-session", lookup)
		if _, duplicate := committed[commitment]; duplicate {
			w.fail()
			return
		}
		committed[commitment] = value
	}
	runtimeProjectionMap(w, committed, func(_ string, v runtimeTeamSession) {
		w.text(v.ID)
		w.text(v.UserID)
		w.instant(v.ExpiresAt)
		w.text(v.OIDCPolicyRevision)
		w.text(v.OIDCBindingKey)
		w.text(v.OAuthPolicyRevision)
		w.text(v.OAuthBindingKey)
		w.text(v.LDAPPolicyRevision)
		w.text(v.LDAPBindingKey)
		w.text(v.SAMLPolicyRevision)
		w.text(v.SAMLBindingKey)
		w.text(v.NamedIdentityPolicyKey)
		w.text(v.NamedIdentityBindingKey)
	})
}
