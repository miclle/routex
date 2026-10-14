package service

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var apiKeyDeliveryControllerID = regexp.MustCompile(`^(?:kcg|kds|vap|vpb|key|pky)_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var apiKeyDeliveryAgentSegment = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

const apiKeyDeliveryControllerJWTFile = "/var/run/secrets/routex/workload.jwt"

func apiKeyDeliveryAgentPath(value string, max int, empty bool) bool {
	if value == "" {
		return empty
	}
	if len(value) > max {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if len(part) > 128 || !apiKeyDeliveryAgentSegment.MatchString(part) || part == "." || part == ".." || strings.HasSuffix(part, ".") {
			return false
		}
	}
	return true
}

func apiKeyDeliveryAgentEndpoint(value string) bool {
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\"\\%?# \t\r\n") || strings.Contains(value, "${") || strings.Contains(value, "%{") || strings.Contains(value, "{{") || strings.Contains(value, "}}") {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Path, "//") {
		return false
	}
	return apiKeyDeliveryAgentPath(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), 256, true)
}

func apiKeyDeliveryAgentConfiguration(d APIKeyDeliveryDescriptorView) (APIKeyDeliveryControllerConfiguration, error) {
	for _, item := range []struct{ value, prefix string }{{d.KeyID, ""}, {d.ProfileID, "vap_"}, {d.PrincipalID, "vpb_"}, {d.SlotID, "kds_"}} {
		if !apiKeyDeliveryControllerID.MatchString(item.value) || item.prefix != "" && !strings.HasPrefix(item.value, item.prefix) {
			return APIKeyDeliveryControllerConfiguration{}, apiKeyDeliveryUnavailable
		}
	}
	if !strings.HasPrefix(d.KeyID, "key_") && !strings.HasPrefix(d.KeyID, "pky_") || d.SchemaVersion != 1 || d.Version != 1 || d.Method != "jwt" || d.DescriptorRevision == 0 || d.DescriptorRevision > uint64(1<<63-1) || d.Role != "routex-api-key-"+d.PrincipalID || !apiKeyDeliveryAgentEndpoint(d.Endpoint) || !apiKeyDeliveryAgentPath(d.Namespace, 256, true) || !apiKeyDeliveryAgentPath(d.Mount, 128, false) || !apiKeyDeliveryAgentPath(d.AuthMount, 128, false) || !apiKeyDeliveryAgentPath(d.Path, 512, false) || !strings.HasSuffix(d.Path, "/"+d.ProfileID+"/routex-api-key-"+d.KeyID) || !apiKeyDeliveryAgentPath(d.DataField, 64, false) || strings.Contains(d.DataField, "/") {
		return APIKeyDeliveryControllerConfiguration{}, apiKeyDeliveryUnavailable
	}
	keyFile := "/var/run/secrets/routex/api-keys/" + d.SlotID
	path := d.Mount + "/data/" + d.Path + "?version=1"
	template := "{{ with secret " + strconv.Quote(path) + " }}{{ $key := index .Data.data " + strconv.Quote(d.DataField) + " }}{{ if not $key }}{{ sprig_fail \"RouteX API Key field is missing or empty\" }}{{ end }}{{ $key }}{{ end }}"
	text := "vault {\n  address = " + strconv.Quote(d.Endpoint) + "\n"
	if d.Namespace != "" {
		text += "  namespace = " + strconv.Quote(d.Namespace) + "\n"
	}
	text += "}\n\nauto_auth {\n  method \"jwt\" {\n    mount_path = " + strconv.Quote("auth/"+d.AuthMount) + "\n    config = {\n      role = " + strconv.Quote(d.Role) + "\n      path = " + strconv.Quote(apiKeyDeliveryControllerJWTFile) + "\n      remove_jwt_after_reading = false\n    }\n  }\n}\n\ntemplate_config {\n  exit_on_retry_failure = true\n}\n\ntemplate {\n  contents = " + strconv.Quote(template) + "\n  destination = " + strconv.Quote(keyFile) + "\n  perms = \"0600\"\n  backup = false\n  error_on_missing_key = true\n}\n"
	if len(text) > 16<<10 {
		return APIKeyDeliveryControllerConfiguration{}, apiKeyDeliveryUnavailable
	}
	return APIKeyDeliveryControllerConfiguration{SchemaVersion: 1, SlotID: d.SlotID, DescriptorRevision: strconv.FormatUint(d.DescriptorRevision, 10), KeyID: d.KeyID, AgentConfiguration: text, JWTFile: apiKeyDeliveryControllerJWTFile, KeyFile: keyFile}, nil
}

func apiKeyDeliveryControllerOwner(tx *gorm.DB, slot entity.APIKeyDeliverySlot) (apiKeyDeliveryOwner, error) {
	o := apiKeyDeliveryOwner{Kind: slot.OwnerKind, ID: slot.OwnerID, Birth: slot.OwnerBirth, Digest: slot.OwnerDigest}
	if !apiKeyDeliveryBirth(o.Birth) || o.Digest != apiKeyDeliveryOwnerDigest(o.Kind, o.ID, o.Birth) {
		return o, apiKeyDeliveryUnavailable
	}
	switch o.Kind {
	case "personal":
		u, err := registrationAdmittedUser(tx, o.ID, true)
		if err != nil {
			return o, err
		}
		if u.ID != o.ID || !u.CreatedAt.Equal(o.Birth) {
			return o, apiKeyDeliveryConflict
		}
	case "project":
		var p entity.Project
		if err := personalExact(vaultDB(tx), "id", o.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&p).Error; err != nil {
			return o, err
		}
		if p.ID != o.ID || !p.CreatedAt.Equal(o.Birth) || p.Status != entity.ResourceActive {
			return o, apiKeyDeliveryConflict
		}
	default:
		return o, apiKeyDeliveryUnavailable
	}
	return o, nil
}

// Machine projection never creates an Authentication or borrows a user Session.
// The governance lock serializes owner, grant and delivery policy mutations.
func (s *Service) apiKeyDeliveryControllerConfiguration(tx *gorm.DB, slotID string) (apiKeyDeliveryControllerConfigurationFacts, error) {
	var facts apiKeyDeliveryControllerConfigurationFacts
	var slot entity.APIKeyDeliverySlot
	if err := personalExact(vaultDB(tx), "id", slotID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&slot).Error; err != nil {
		return facts, err
	}
	if slot.ID != slotID || !apiKeyDeliveryBirth(slot.CreatedAt) || slot.DescriptorRevision == 0 || slot.SlotDigest != apiKeyDeliveryDigest("api-key-delivery:slot:v1", struct{ Owner, Profile, Chain string }{slot.OwnerDigest, slot.ProfileID, slot.ChainID}) {
		return facts, apiKeyDeliveryUnavailable
	}
	o, err := apiKeyDeliveryControllerOwner(tx, slot)
	if err != nil {
		return facts, err
	}
	p, principal, err := apiKeyDeliveryProfile(tx, slot.ProfileID)
	if err != nil {
		return facts, err
	}
	if !p.CreatedAt.Equal(slot.ProfileBirth) || !apiKeyDeliveryOwnerMatches(p, o) {
		return facts, apiKeyDeliveryConflict
	}
	policy, err := apiKeyDeliveryPolicy(tx, true)
	if err != nil {
		return facts, err
	}
	if !policy.AllowManagedVault {
		return facts, apiKeyDeliveryUnavailable
	}
	x, err := s.apiKeyDeliverySnapshot(tx, policy, true)
	if err != nil {
		return facts, err
	}
	att, err := apiKeyDeliveryReady(tx, p, principal, x)
	if err != nil {
		return facts, err
	}
	var binding entity.APIKeyVaultDeliveryBinding
	if err = personalExact(vaultDB(tx), "slot_id", slot.ID).Where("descriptor_revision = ?", slot.DescriptorRevision).Take(&binding).Error; err != nil {
		return facts, err
	}
	if binding.SlotID != slot.ID || binding.DescriptorRevision != slot.DescriptorRevision || !apiKeyDeliveryBindingCurrent(binding, p, principal, att) || !apiKeyDeliveryBirth(binding.CreatedAt) {
		return facts, apiKeyDeliveryConflict
	}
	if _, err = apiKeyDeliveryAcknowledged(tx, o.Kind, binding.KeyID, binding.KeyBirth); err != nil {
		return facts, err
	}
	var commands []entity.APIKeyDeliveryCommand
	if err = personalExact(vaultDB(tx), "binding_id", binding.ID).Limit(2).Find(&commands).Error; err != nil {
		return facts, err
	}
	if len(commands) != 1 {
		return facts, apiKeyDeliveryUnavailable
	}
	c := commands[0]
	if c.BindingID != binding.ID || c.RuntimeStatus != "confirmed" {
		return facts, apiKeyDeliveryConflict
	}
	r, err := apiKeyDeliveryLoadKey(tx, o, c, false)
	if err != nil {
		return facts, err
	}
	now := time.Now().UTC()
	project := ""
	if o.Kind == "project" {
		project = o.ID
	}
	if r.Key.Status != entity.KeyActive || r.Key.ExpiresAt != nil && !now.Before(*r.Key.ExpiresAt) || !s.apiKeyDeliveryPublished(r.Key, project, r.Models) {
		return facts, apiKeyDeliveryConflict
	}
	d, err := s.apiKeyDeliveryDescriptor(tx, c, binding)
	if err != nil {
		return facts, err
	}
	view, err := apiKeyDeliveryAgentConfiguration(*d)
	if err != nil {
		return facts, err
	}
	facts = apiKeyDeliveryControllerConfigurationFacts{View: view, Owner: o, SlotBirth: slot.CreatedAt, ProfileID: p.ID, ProfileBirth: p.CreatedAt, PrincipalID: principal.ID, PrincipalBirth: principal.CreatedAt, BindingID: binding.ID, BindingBirth: binding.CreatedAt, KeyBirth: binding.KeyBirth, SecurityDigest: apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, principal))}
	facts.View.ConfigurationDigest = apiKeyDeliveryDigest("api-key-delivery:controller-configuration:v1", facts)
	if !personalModelETag(facts.View.ConfigurationDigest) {
		return facts, apperrors.ErrInternal
	}
	return facts, tx.Statement.Context.Err()
}
