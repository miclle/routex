package handler

import (
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/service"
)

func TestAPIKeyDeliveryJSONExactFiniteInputs(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		target    any
	}{
		{"policy", `{"allowed_modes":["manual"],"default_mode":"manual","integration_id":"","writer_identity_id":"","provisioner_identity_id":"","reason":"Reviewed"}`, &service.APIKeyDeliveryPolicyInput{}},
		{"identity", `{"purpose":"writer","integration_id":"vin_owned","token":"private-token","reason":"Reviewed"}`, &service.APIKeyDeliveryIdentityInput{}},
		{"template", `{"auth_mount":"jwt","issuer":"https://issuer.example","jwks_url":"https://issuer.example/keys","audience":"app","allowed_subjects":["owner"],"token_ttl_seconds":60,"token_max_ttl_seconds":300,"reason":"Reviewed"}`, &service.APIKeyCallerTemplateInput{}},
		{"reason", `{"reason":"Reviewed"}`, &apiKeyDeliveryReasonInput{}},
		{"resolve_server_allocates", `{"request_id":"11111111-1111-4111-8111-111111111111","application_name":"App","environment":"prod","principal":{"method":"jwt","subject":"owner"},"reason":"Reviewed"}`, &service.APIKeyDeliveryResolveInput{}},
		{"resolve_explicit_empty", `{"request_id":"11111111-1111-4111-8111-111111111111","consumer_id":"","application_name":"App","environment":"prod","principal":{"method":"jwt","subject":"owner"},"reason":"Reviewed"}`, &service.APIKeyDeliveryResolveInput{}},
		{"approval_false_is_explicit", `{"approved":false,"control_verified":false,"reason":"Reviewed"}`, &service.APIKeyDeliveryApprovalInput{}},
		{"isolation_false_is_explicit", `{"isolated":false,"reason":"Reviewed"}`, &service.APIKeyDeliveryIsolationInput{}},
		{"reconcile", `{"request_id":"11111111-1111-4111-8111-111111111111","reason":"Reviewed"}`, &service.APIKeyDeliveryReconcileInput{}},
		{"managed_nullable_expiry", `{"name":"App 😀","model_ids":["mdl_owned"],"expires_at":null,"request_id":"11111111-1111-4111-8111-111111111111","reason":"Reviewed","delivery":{"consumer_id":"owned","environment":"prod","principal":{"method":"jwt","subject":"owner"},"target_review":"` + strings.Repeat("a", 64) + `"}}`, &service.ManagedAPIKeyInput{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := decodeAPIKeyDeliveryJSON([]byte(tc.raw), tc.target); e != nil {
				t.Fatal("valid finite wire input rejected")
			}
			if v, ok := tc.target.(*service.APIKeyDeliveryResolveInput); ok && v.ConsumerID != "" {
				t.Fatal("consumer allocation was replaced by client value")
			}
			if v, ok := tc.target.(*service.APIKeyDeliveryIdentityInput); ok && v.Token != "private-token" {
				t.Fatal("transient identity body not delivered exactly")
			}
		})
	}
}

func TestAPIKeyDeliveryJSONRejectsNestedDiscardedIntent(t *testing.T) {
	good := `{"name":"App","model_ids":["mdl_owned"],"expires_at":null,"request_id":"11111111-1111-4111-8111-111111111111","reason":"Reviewed","delivery":{"consumer_id":"owned","environment":"prod","principal":{"method":"jwt","subject":"owner"},"target_review":"` + strings.Repeat("a", 64) + `"}}`
	cases := map[string]string{
		"non_object": `[]`, "null_root": `null`, "trailing": good + `{}`, "missing_delivery": strings.Replace(good, `,"delivery":`+good[strings.Index(good, `{"consumer_id"`):len(good)-1], "", 1),
		"top_duplicate":     strings.Replace(good, `"name":"App"`, `"name":"App","name":"Other"`, 1),
		"decoded_duplicate": strings.Replace(good, `"subject":"owner"`, `"subject":"owner","\u0073ubject":"other"`, 1),
		"nested_duplicate":  strings.Replace(good, `"environment":"prod"`, `"environment":"prod","environment":"other"`, 1),
		"case_alias":        strings.Replace(good, `"subject"`, `"Subject"`, 1),
		"secret_override":   strings.Replace(good, `"subject":"owner"`, `"subject":"owner","secret":"private"`, 1),
		"path_override":     strings.Replace(good, `"consumer_id":"owned"`, `"consumer_id":"owned","path":"private"`, 1),
		"owner_override":    strings.Replace(good, `"name":"App"`, `"name":"App","owner_id":"private"`, 1),
		"principal_null":    strings.Replace(good, `{"method":"jwt","subject":"owner"}`, `null`, 1),
		"principal_array":   strings.Replace(good, `{"method":"jwt","subject":"owner"}`, `[]`, 1),
		"subject_null":      strings.Replace(good, `"subject":"owner"`, `"subject":null`, 1),
		"subject_integer":   strings.Replace(good, `"subject":"owner"`, `"subject":1`, 1),
		"models_null":       strings.Replace(good, `["mdl_owned"]`, `null`, 1),
		"model_null":        strings.Replace(good, `["mdl_owned"]`, `[null]`, 1),
		"model_nested":      strings.Replace(good, `["mdl_owned"]`, `[["mdl_owned"]]`, 1),
		"expiry_boolean":    strings.Replace(good, `"expires_at":null`, `"expires_at":false`, 1),
		"expiry_malformed":  strings.Replace(good, `"expires_at":null`, `"expires_at":"not-a-time"`, 1),
		"invalid_utf8":      strings.Replace(good, "App", string([]byte{0xff}), 1),
		"high_surrogate":    strings.Replace(good, "App", `\ud800`, 1),
		"low_surrogate":     strings.Replace(good, "App", `\udfff`, 1),
		"invalid_pair":      strings.Replace(good, "App", `\ud800\u0041`, 1),
		"oversized_string":  strings.Replace(good, "App", strings.Repeat("x", 4097), 1),
		"oversized_reason":  strings.Replace(good, "Reviewed", strings.Repeat("x", 1025), 1),
		"oversized_array":   strings.Replace(good, `["mdl_owned"]`, `[`+strings.TrimSuffix(strings.Repeat(`"m",`, 129), ",")+`]`, 1),
		"oversized_body":    strings.Repeat(" ", 64<<10) + good,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var in service.ManagedAPIKeyInput
			if decodeAPIKeyDeliveryJSON([]byte(raw), &in) == nil {
				t.Fatal("discarded or ambiguous managed intent accepted")
			}
		})
	}
	for _, raw := range []string{`{"isolated":null,"reason":"R"}`, `{"reason":"R"}`, `{"isolated":"true","reason":"R"}`, `{"isolated":true,"reason":"R","isolated":false}`} {
		var in service.APIKeyDeliveryIsolationInput
		if decodeAPIKeyDeliveryJSON([]byte(raw), &in) == nil {
			t.Fatal("isolation shape accepted without exact explicit boolean")
		}
	}
}

func TestAPIKeyDeliveryJSONCanonicalTTLAndUnicode(t *testing.T) {
	raw := `{"auth_mount":"jwt","issuer":"https://issuer.example","jwks_url":"https://issuer.example/keys","audience":"app","allowed_subjects":["owner"],"token_ttl_seconds":60,"token_max_ttl_seconds":300,"reason":"Reviewed"}`
	for _, value := range []string{`null`, `"60"`, `60.0`, `6e1`, `-0`, `-1`, `18446744073709551616`, `true`} {
		var in service.APIKeyCallerTemplateInput
		if decodeAPIKeyDeliveryJSON([]byte(strings.Replace(raw, `"token_ttl_seconds":60`, `"token_ttl_seconds":`+value, 1)), &in) == nil {
			t.Fatal("noncanonical TTL accepted")
		}
	}
	for _, name := range []string{`\ud83d\ude00`, `\ufffd`, `literal \\ud800`} {
		var in apiKeyDeliveryReasonInput
		if decodeAPIKeyDeliveryJSON([]byte(`{"reason":"`+name+`"}`), &in) != nil {
			t.Fatal("valid Unicode or literal escape rejected")
		}
	}
	var wrong struct{ Reason string }
	if decodeAPIKeyDeliveryJSON([]byte(`{"reason":"R"}`), &wrong) == nil {
		t.Fatal("open-ended input target admitted")
	}
}

func TestAPIKeyDeliveryTargetsQueryIsClosedAndBounded(t *testing.T) {
	for _, tc := range []struct {
		raw, cursor string
		limit       int
	}{{"", "", 20}, {"limit=1", "", 1}, {"limit=100&cursor=vap_owned", "vap_owned", 100}} {
		cursor, limit, e := apiKeyDeliveryTargetQuery(tc.raw, false)
		if e != nil || cursor != tc.cursor || limit != tc.limit {
			t.Fatal("valid bounded query changed")
		}
	}
	for _, raw := range []string{"unknown=1", "limit=", "cursor=", "limit=20&limit=20", "cursor=a&cursor=b", "limit=020", "limit=+20", "limit=0", "limit=101", "limit=-1", "limit=2.0", "cursor=%zz", "cursor=a;b", "cursor=%00", "cursor=%E4%B8%AD", "cursor=a%3Ab", "cursor=" + strings.Repeat("x", 31)} {
		if _, _, e := apiKeyDeliveryTargetQuery(raw, false); e == nil {
			t.Fatal("discarded target query accepted")
		}
	}
	if _, _, e := apiKeyDeliveryTargetQuery("", true); e == nil {
		t.Fatal("ForceQuery accepted")
	}
}
