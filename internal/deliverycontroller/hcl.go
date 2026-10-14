package deliverycontroller

import (
	"errors"
	"strconv"
	"strings"
)

type configuration struct {
	SchemaVersion       int    `json:"schema_version"`
	SlotID              string `json:"slot_id"`
	DescriptorRevision  string `json:"descriptor_revision"`
	KeyID               string `json:"key_id"`
	ConfigurationDigest string `json:"configuration_digest"`
	AgentConfiguration  string `json:"agent_configuration"`
	JWTFile             string `json:"jwt_file"`
	KeyFile             string `json:"key_file"`
}

func (c configuration) validate(cfg Config) error {
	if c.SchemaVersion != 1 || c.SlotID != cfg.SlotID || c.DescriptorRevision != cfg.DescriptorRevision || c.ConfigurationDigest != cfg.ConfigurationDigest || !keyPattern.MatchString(c.KeyID) || c.JWTFile != jwtFile || c.KeyFile != keyDirectory+"/"+cfg.SlotID || len(c.AgentConfiguration) == 0 || len(c.AgentConfiguration) > maxHCLBytes {
		return errors.New("controller configuration does not match pinned target")
	}
	return validateHCL(c, cfg.AllowLoopbackHTTP)
}

// This is a positive parser for the one canonical generated configuration. It
// never accepts general HCL, server-provided programs or additional directives.
func validateHCL(c configuration, allowLoopbackHTTP bool) error {
	p := hclCursor{text: c.AgentConfiguration}
	p.literal("vault {\n  address = ")
	address := p.quoted()
	p.literal("\n")
	if p.starts("  namespace = ") {
		p.literal("  namespace = ")
		if !agentPath(p.quoted(), 256, false) {
			p.invalid = true
		}
		p.literal("\n")
	}
	p.literal("}\n\nauto_auth {\n  method \"jwt\" {\n    mount_path = ")
	authMount := p.quoted()
	p.literal("\n    config = {\n      role = ")
	role := p.quoted()
	p.literal("\n      path = ")
	if p.quoted() != jwtFile {
		p.invalid = true
	}
	p.literal("\n      remove_jwt_after_reading = false\n    }\n  }\n}\n\ntemplate_config {\n  exit_on_retry_failure = true\n}\n\ntemplate {\n  contents = ")
	contents := p.quoted()
	p.literal("\n  destination = ")
	if p.quoted() != c.KeyFile {
		p.invalid = true
	}
	p.literal("\n  perms = \"0600\"\n  backup = false\n  error_on_missing_key = true\n}\n")
	if p.invalid || p.pos != len(p.text) {
		return errors.New("unsupported Agent configuration structure")
	}
	if _, err := endpoint(address, allowLoopbackHTTP); err != nil || !strings.HasPrefix(authMount, "auth/") || !agentPath(strings.TrimPrefix(authMount, "auth/"), 128, false) || !strings.HasPrefix(role, "routex-api-key-vpb_") || !prefixedIdentity(strings.TrimPrefix(role, "routex-api-key-"), "vpb_") {
		return errors.New("invalid Agent authentication target")
	}
	return validateTemplate(contents, c.KeyID)
}

func validateTemplate(contents, keyID string) error {
	p := hclCursor{text: contents}
	p.literal("{{ with secret ")
	path := p.quoted()
	p.literal(" }}{{ $key := index .Data.data ")
	field := p.quoted()
	p.literal(" }}{{ if not $key }}{{ sprig_fail \"RouteX API Key field is missing or empty\" }}{{ end }}{{ $key }}{{ end }}")
	if p.invalid || p.pos != len(p.text) || !strings.HasSuffix(path, "?version=1") || !agentPath(field, 64, false) || strings.Contains(field, "/") {
		return errors.New("invalid Agent template")
	}
	segments := strings.Split(strings.TrimSuffix(path, "?version=1"), "/")
	if len(segments) < 4 || !agentPath(strings.TrimSuffix(path, "?version=1"), 1024, false) || segments[len(segments)-1] != "routex-api-key-"+keyID || !prefixedIdentity(segments[len(segments)-2], "vap_") {
		return errors.New("invalid exact-version Key target")
	}
	// The mount may contain multiple components; a single data separator must
	// follow a nonempty mount and precede the exact private record path.
	data := -1
	for i, segment := range segments[:len(segments)-2] {
		if segment == "data" {
			if data != -1 {
				return errors.New("ambiguous KV-v2 mount")
			}
			data = i
		}
	}
	if data < 1 || data >= len(segments)-2 {
		return errors.New("missing KV-v2 mount")
	}
	return nil
}

func prefixedIdentity(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && slotPattern.MatchString("kds_"+strings.TrimPrefix(value, prefix))
}

type hclCursor struct {
	text    string
	pos     int
	invalid bool
}

func (p *hclCursor) starts(value string) bool {
	return !p.invalid && strings.HasPrefix(p.text[p.pos:], value)
}

func (p *hclCursor) literal(value string) {
	if !p.starts(value) {
		p.invalid = true
		return
	}
	p.pos += len(value)
}

func (p *hclCursor) quoted() string {
	if !p.starts("\"") {
		p.invalid = true
		return ""
	}
	start := p.pos
	p.pos++
	for p.pos < len(p.text) {
		if p.text[p.pos] == '\\' {
			p.pos += 2
			continue
		}
		if p.text[p.pos] == '"' {
			p.pos++
			literal := p.text[start:p.pos]
			value, err := strconv.Unquote(literal)
			// Canonical rendering rules out alternative escapes, interpolation
			// tricks, comments and hidden differences in reviewed configuration.
			if err != nil || strconv.Quote(value) != literal {
				p.invalid = true
				return ""
			}
			return value
		}
		p.pos++
	}
	p.invalid = true
	return ""
}
