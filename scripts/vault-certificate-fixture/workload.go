package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"

	"github.com/miclle/routex/pkg/vault"
)

// The owner runs this only against the freshly initialized loopback Vault from
// its coordinator. No deployment credentials, unnamed role or KV write is used.
func (o *fixtureOwner) workload() error {
	o.stage = "load-workload-materials"
	files, err := loadMaterials()
	if err != nil {
		return err
	}
	for _, name := range []string{"outsider.pem", "outsider.key", "cnless.pem", "cnless.key"} {
		files[name], err = privateInput("/input/"+name, 16<<10)
		if err != nil {
			return err
		}
	}
	o.stage = "load-initialization"
	raw, err := privateInput("/vault/logs/discovery-"+BuildRunID+"/private-initialization.json", 32768)
	var initialization struct {
		Token string `json:"root_token"`
	}
	if err != nil || json.Unmarshal(raw, &initialization) != nil || len(initialization.Token) < 16 {
		return errFixture
	}
	mount := "routex-cert-" + BuildRunID[:16]
	if o.control("enable-cert", "POST", "/sys/auth/"+mount, initialization.Token, map[string]string{"type": "cert"}, 204) != nil {
		return errFixture
	}
	for _, role := range []string{"writer", "reader"} {
		policies := []string{"discovery-reader"}
		if role == "writer" {
			policies = []string{"discovery-writer", "discovery-cleanup"}
		}
		if o.control("role-"+role, "POST", "/auth/"+mount+"/certs/"+role, initialization.Token, map[string]any{
			"certificate": string(files["client-ca.pem"]), "allowed_uri_sans": []string{workloadURI(BuildRunID, role)},
			"token_policies": policies, "token_ttl": 120, "token_max_ttl": 120, "token_explicit_max_ttl": 120, "token_no_default_policy": true,
		}, 204) != nil {
			return errFixture
		}
	}
	for _, test := range []struct {
		name, identity, role string
		leafOnly, allowed    bool
		status               int
	}{
		{"writer-chain", "writer", "writer", false, true, 0},
		{"reader-chain", "reader", "reader", false, true, 0},
		{"missing-client-intermediate", "writer", "writer", true, false, 400},
		{"reader-cannot-be-writer", "reader", "writer", false, false, 400},
		{"other-trust-domain", "outsider", "writer", false, false, 400},
		// Vault 2.2 cert auth needs a nonempty CN for its identity alias even
		// after a valid URI constraint. Keep this incompatibility explicit.
		{"empty-cn-alias-refused", "cnless", "writer", false, false, 500},
	} {
		o.stage = test.name
		before, err := o.audit()
		if err != nil || o.save(test.name+"-audit-before.jsonl", before) != nil {
			return errFixture
		}
		certificate := files[test.identity+".pem"]
		if test.leafOnly {
			block, rest := pem.Decode(certificate)
			if block == nil || len(rest) == 0 {
				return errFixture
			}
			certificate = pem.EncodeToMemory(block)
		}
		client, err := vault.NewCertificate(vault.Descriptor{Endpoint: "https://127.0.0.1:8201", Mount: "kv", Prefix: "discovery", DataField: "value"}, true, certificate, files[test.identity+".key"], files["server-ca.pem"])
		if err != nil {
			return errFixture
		}
		token, observation, loginErr := client.LoginCertificate(o.ctx, mount, test.role)
		client.Close()
		hasToken := token != nil
		if token != nil {
			_, err = token.Token()
			token.Close()
		}
		if o.save(test.name+"-result.json", map[string]any{"observation": observation, "token_present": hasToken, "allowed": test.allowed}) != nil {
			return errFixture
		}
		tokenErr := err
		after, err := o.audit()
		if err != nil || !bytes.HasPrefix(after, before) || o.save(test.name+"-audit-after.jsonl", after) != nil {
			return errFixture
		}
		// One native login pair, no fallback to another role and no KV request.
		lines := bytes.Split(bytes.TrimSuffix(after[len(before):], []byte("\n")), []byte("\n"))
		if len(lines) != 2 {
			return errFixture
		}
		var requestID string
		for index, line := range lines {
			var entry struct {
				Type    string `json:"type"`
				Request struct {
					ID, Path string
				} `json:"request"`
			}
			if json.Unmarshal(line, &entry) != nil || entry.Request.Path != "auth/"+mount+"/login" || entry.Request.ID == "" || index == 0 && entry.Type != "request" || index == 1 && (entry.Type != "response" || entry.Request.ID != requestID) {
				return errFixture
			}
			requestID = entry.Request.ID
		}
		if test.allowed {
			if loginErr != nil || tokenErr != nil || !hasToken || !observation.Succeeded || observation.Failure != nil {
				return errFixture
			}
		} else if loginErr == nil || hasToken || observation.Succeeded || observation.Failure == nil || observation.Failure.Code != "http_status" || observation.Failure.HTTPStatus != test.status {
			return errFixture
		}
	}
	return nil
}
