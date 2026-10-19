package service

import (
	"encoding/base32"
	"net/url"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
)

func configurationIDV1(id, prefix string) bool {
	return strings.HasPrefix(id, prefix+"_") && configurationCanonicalIDV1.MatchString(id)
}
func configurationOptionalIDV1(id *string, prefix string) bool {
	return id == nil || configurationIDV1(*id, prefix)
}
func configurationBirthsV1[T any](rows []T, prefix string, identity func(T) (string, time.Time)) bool {
	for _, row := range rows {
		id, birth := identity(row)
		if !configurationIDV1(id, prefix) || birth.IsZero() {
			return false
		}
	}
	return true
}

// Shape validation preserves canonical stale and dangling source references. It
// never substitutes present target rows or derives current authority.
func configurationReferencesV1(s runtimeConfigurationSourceV1, p *runtimeConfigurationPricingV1) bool {
	ok := configurationBirthsV1(s.Users, "usr", func(v runtimeConfigurationUserV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Models, "mdl", func(v runtimeConfigurationModelV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Providers, "prv", func(v runtimeConfigurationProviderV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Connections, "con", func(v runtimeConfigurationProviderConnectionV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.ProviderModels, "pmd", func(v runtimeConfigurationProviderModelV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Credentials, "crd", func(v runtimeConfigurationProviderCredentialV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Keys, "key", func(v runtimeConfigurationAPIKeyV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(s.Egresses, "egr", func(v runtimeConfigurationEgressV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	for _, row := range s.Users {
		if !configurationOptionalIDV1(row.ApprovalApplicationID, "raa") {
			return false
		}
	}
	for _, row := range s.ApprovalApplications {
		if !configurationIDV1(row.ID, "raa") || !configurationIDV1(row.UserID, "usr") || !runtimeRouteDigest.MatchString(row.Revision) {
			return false
		}
	}
	for _, row := range s.Keys {
		if !configurationIDV1(row.UserID, "usr") || !configurationOptionalIDV1(row.ReplacesKeyID, "key") || !runtimeRouteDigest.MatchString(row.TokenHash) {
			return false
		}
	}
	for _, row := range s.Scopes {
		if !configurationIDV1(row.KeyID, "key") || !configurationIDV1(row.ModelID, "mdl") {
			return false
		}
	}
	for _, row := range s.Grants {
		if !configurationIDV1(row.UserID, "usr") || !configurationIDV1(row.ModelID, "mdl") {
			return false
		}
	}
	for _, row := range s.Names {
		if row.Name == "" || !configurationIDV1(row.ModelID, "mdl") || !configurationOptionalIDV1(row.CurrentModelID, "mdl") {
			return false
		}
	}
	for _, row := range s.Connections {
		if !configurationIDV1(row.ProviderID, "prv") || !entity.SupportedNativeProtocol(row.Protocol) || !configurationOptionalIDV1(row.EgressID, "egr") {
			return false
		}
		u, err := url.Parse(row.BaseURL)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
			return false
		}
		switch row.EgressMode {
		case "", "default", "direct":
			if row.EgressID != nil {
				return false
			}
		case "proxy":
			if row.EgressID == nil {
				return false
			}
		default:
			return false
		}
	}
	for _, row := range s.ProviderModels {
		if !configurationIDV1(row.ConnectionID, "con") {
			return false
		}
	}
	for _, row := range s.Credentials {
		if !configurationIDV1(row.ConnectionID, "con") || row.CoverageRevision < 0 || !configurationOptionalIDV1(row.ReplacesCredentialID, "crd") {
			return false
		}
		switch row.StorageSource {
		case "", "inline":
			if row.VaultReference != nil {
				return false
			}
		case "vault":
			if r := row.VaultReference; r != nil {
				if r.CredentialID != row.ID || !vaultProbeID.MatchString(r.ReferenceID) || !vaultIntegrationID.MatchString(r.IntegrationID) || !vaultRevisionID.MatchString(r.RevisionID) || !runtimeRouteDigest.MatchString(r.ExpectedMarkerSHA256) || !runtimeRouteDigest.MatchString(r.DescriptorSHA256) {
					return false
				}
			}
		default:
			return false
		}
	}
	for _, row := range s.Access {
		if !configurationIDV1(row.CredentialID, "crd") || !configurationIDV1(row.ProviderModelID, "pmd") {
			return false
		}
	}
	// Attestations deliberately retain historical deleted targets; shape is not
	// current eligibility, and their absence from a live table is not backfilled.
	for _, row := range s.Attestations {
		if !configurationIDV1(row.CredentialID, "crd") || !configurationIDV1(row.ProviderModelID, "pmd") || !runtimeRouteDigest.MatchString(row.Identity) {
			return false
		}
	}
	for _, row := range s.Bindings {
		if !configurationIDV1(row.ID, "bnd") || !configurationIDV1(row.ModelID, "mdl") || !configurationIDV1(row.ProviderModelID, "pmd") || row.Weight < 0 || row.Weight > 100 {
			return false
		}
	}
	if s.EgressSetting.ID != 1 || !configurationOptionalIDV1(s.EgressSetting.DefaultEgressID, "egr") {
		return false
	}
	for _, row := range s.Egresses {
		if row.Port < 1 || row.Port > 65535 || row.Host == "" || row.Kind != "https" && row.Kind != "socks5" {
			return false
		}
	}
	if !configurationQuotaV1(s.Quota) {
		return false
	}
	for _, row := range s.Limits {
		if !configurationScopeV1(row.ScopeKind, row.ScopeID) {
			return false
		}
		for _, bound := range []*int64{row.Tokens5H, row.Tokens7D, row.TokensMonth, row.TPM, row.RPM, row.Concurrency} {
			if bound != nil && (*bound < 0 || *bound > limits.MaxInteger) {
				return false
			}
		}
	}
	for account, policy := range s.LimitPolicies {
		if !configurationAccountV1(account) {
			return false
		}
		if _, err := limits.Normalize(limits.Policy(policy)); err != nil {
			return false
		}
	}
	if !configurationRotationRootsV1(s) {
		return false
	}
	if !configurationProjectV1(s.ProjectData) || !configurationTeamsV1(s.TeamSessionData) {
		return false
	}
	return configurationPricingV1(p)
}

func configurationScopeV1(kind, id string) bool {
	switch kind {
	case "user":
		return configurationIDV1(id, "usr")
	case "project":
		return configurationIDV1(id, "prj")
	case "team":
		return configurationIDV1(id, "tea")
	case "key":
		return configurationIDV1(id, "key") || configurationIDV1(id, "pky")
	case "team_member":
		raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(id)
		return err == nil && len(raw) == 32 && base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw) == id
	}
	return false
}
func configurationAccountV1(account string) bool {
	// team_member has an underscore within the kind.
	if strings.HasPrefix(account, "team_member_") {
		return configurationScopeV1("team_member", strings.TrimPrefix(account, "team_member_"))
	}
	kind, id, ok := strings.Cut(account, "_")
	return ok && configurationScopeV1(kind, id)
}
func configurationQuotaV1(q *runtimeConfigurationQuotaV1) bool {
	if q == nil {
		return true
	}
	if q.Setting.ID != 1 || !pricing.Currency(q.Currency) {
		return false
	}
	if _, err := time.LoadLocation(q.Setting.TimeZone); err != nil {
		return false
	}
	for id, b := range q.Bounds {
		if !configurationIDV1(id, "pmd") || id != b.ProviderModelID || !entity.SupportedNativeProtocol(b.Protocol) || b.MaxInputTokens <= 0 || b.MaxInputTokens > limits.MaxInteger || b.MaxOutputTokens <= 0 || b.MaxOutputTokens > limits.MaxInteger {
			return false
		}
	}
	for account, birth := range q.Created {
		if !configurationAccountV1(account) || birth.IsZero() {
			return false
		}
	}
	for account := range q.Revisions {
		if !configurationAccountV1(account) {
			return false
		}
	}
	return true
}
func configurationProjectV1(d *runtimeConfigurationProjectDataV1) bool {
	if d == nil {
		return true
	}
	ok := configurationBirthsV1(d.Projects, "prj", func(v runtimeConfigurationProjectV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	ok = configurationBirthsV1(d.Keys, "pky", func(v runtimeConfigurationProjectKeyV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	for _, r := range d.Managers {
		// Earlier offboarding writes used pjm. Preserve that exact historical
		// identity in detached source; new relationships use pmg. Neither codec
		// acceptance nor routing rollback grants management authority.
		managerID := configurationIDV1(r.ID, "pmg") || configurationIDV1(r.ID, "pjm")
		if !managerID || !configurationIDV1(r.UserID, "usr") || !configurationIDV1(r.ProjectID, "prj") {
			return false
		}
	}
	for _, r := range d.Keys {
		if !configurationIDV1(r.ProjectID, "prj") || !configurationIDV1(r.CreatorID, "usr") || !configurationOptionalIDV1(r.ReplacesKeyID, "pky") || !runtimeRouteDigest.MatchString(r.TokenHash) {
			return false
		}
	}
	for _, r := range d.Scopes {
		if !configurationIDV1(r.KeyID, "pky") || !configurationIDV1(r.ModelID, "mdl") {
			return false
		}
	}
	for _, r := range d.Grants {
		if !configurationIDV1(r.ProjectID, "prj") || !configurationIDV1(r.ModelID, "mdl") {
			return false
		}
	}
	return true
}
func configurationTeamsV1(d *runtimeConfigurationTeamSessionDataV1) bool {
	if d == nil {
		return true
	}
	policy := d.AuthenticationPolicy
	if !runtimeRouteDigest.MatchString(policy.Revision) || !policy.Enforced && (policy.EmergencyAdminID != "" || policy.EmergencyAdminCreatedAt != nil) || policy.Enforced && (!configurationIDV1(policy.EmergencyAdminID, "usr") || policy.EmergencyAdminCreatedAt == nil || policy.EmergencyAdminCreatedAt.IsZero()) {
		return false
	}
	ok := configurationBirthsV1(d.Teams, "tea", func(v runtimeConfigurationTeamV1) (string, time.Time) { return v.ID, v.CreatedAt })
	if !ok {
		return false
	}
	for _, r := range d.Memberships {
		if !configurationIDV1(r.ID, "tmm") || !configurationIDV1(r.TeamID, "tea") || !configurationIDV1(r.UserID, "usr") {
			return false
		}
	}
	for _, r := range d.Grants {
		if !configurationIDV1(r.TeamID, "tea") || !configurationIDV1(r.ModelID, "mdl") {
			return false
		}
	}
	for _, r := range d.Sessions {
		if !configurationIDV1(r.ID, "ses") || !configurationIDV1(r.UserID, "usr") || !runtimeRouteDigest.MatchString(r.TokenHash) || !r.CreatedAt.Before(r.ExpiresAt) {
			return false
		}
	}
	for id, r := range d.EmergencyUsers {
		if id != r.ID || !configurationIDV1(id, "usr") || !runtimeRouteDigest.MatchString(r.PasswordDigest) {
			return false
		}
	}
	for id, r := range d.EmergencyMFA {
		if id != r.UserID || !configurationIDV1(id, "usr") || !runtimeProjectionMFAGeneration.MatchString(r.Generation) {
			return false
		}
	}
	return configurationIdentitySourcesV1(d)
}
func configurationPricingV1(p *runtimeConfigurationPricingV1) bool {
	if p == nil {
		return true
	}
	if p.Setting.ID != 1 || !pricing.Currency(p.Setting.PlatformCurrency) {
		return false
	}
	for _, r := range p.Prices {
		if !configurationIDV1(r.ID, "prc") || !configurationIDV1(r.ProviderModelID, "pmd") || r.ContextThreshold < 0 {
			return false
		}
	}
	for _, r := range p.FX {
		if !pricing.Currency(r.Currency) {
			return false
		}
		if _, err := pricing.Decimal(r.Rate); err != nil {
			return false
		}
	}
	for _, r := range p.Rates {
		if !configurationIDV1(r.ID, "rat") || !configurationIDV1(r.ModelPriceID, "prc") || !pricing.Currency(r.Currency) {
			return false
		}
		if pricing.ValidateRate(pricing.Rate{ID: r.ID, Metric: r.Metric, Tier: r.Tier, Unit: r.Unit, Currency: r.Currency, Amount: r.Amount, Enabled: r.Enabled}) != nil {
			return false
		}
	}
	return true
}

func configurationRotationRootsV1(s runtimeConfigurationSourceV1) bool {
	for id, root := range s.LimitRoots {
		switch {
		case configurationIDV1(id, "key"):
			if !configurationIDV1(root, "key") {
				return false
			}
		case configurationIDV1(id, "pky"):
			if !configurationIDV1(root, "pky") {
				return false
			}
		default:
			return false
		}
	}
	return true
}
