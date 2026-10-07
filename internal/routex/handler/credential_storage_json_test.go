package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/service"
)

func TestCredentialCreationContextUsesBodyRawETagNotQuotedHeader(t *testing.T) {
	// This is the actual GET DTO -> create decoder boundary. The real-driver
	// lifecycle separately exercises both HTTP headers and persistence.
	raw := strings.Repeat("a", 64)
	contextJSON, _ := json.Marshal(service.CredentialStorageContext{StorageSource: "vault", ETag: raw})
	var context service.CredentialStorageContext
	if json.Unmarshal(contextJSON, &context) != nil {
		t.Fatal("context wire")
	}
	for _, typ := range []string{"provider", "connection", "credential"} {
		t.Run(typ, func(t *testing.T) {
			target := map[string]any{"provider": &CreateProviderRequest{}, "connection": &CreateConnectionRequest{}, "credential": &CreateCredentialRequest{}}[typ]
			body := `{"request_id":"11111111-1111-4111-8111-111111111111","storage_policy_etag":` + strconv.Quote(context.ETag) + `}`
			if json.Unmarshal([]byte(body), target) != nil {
				t.Fatal("raw body ETag rejected")
			}
			for _, bad := range []string{strconv.Quote(strconv.Quote(raw)), `null`, `[]`, `""`, `"abc"`} {
				body := `{"storage_policy_etag":` + bad + `}`
				if json.Unmarshal([]byte(body), target) == nil {
					t.Fatal("invalid review token accepted", bad)
				}
			}
		})
	}
}
func TestCredentialCreationStrictUUIDAndDuplicateReview(t *testing.T) {
	for _, bad := range []string{`{"request_id":null}`, `{"request_id":123}`, `{"request_id":"11111111-1111-1111-8111-111111111111"}`, `{"request_id":"11111111-1111-4111-8111-111111111111","request_id":"22222222-2222-4222-8222-222222222222"}`, `{"storage_source":"inline"}`, `{} {}`} {
		var v CreateCredentialRequest
		if json.Unmarshal([]byte(bad), &v) == nil {
			t.Fatal("malformed creation accepted", bad)
		}
	}
	var legacy CreateCredentialRequest
	if json.Unmarshal([]byte(`{"name":"legacy","secret":"transient","priority":0}`), &legacy) != nil {
		t.Fatal("legacy inline DTO rejected")
	}
}

func TestCredentialCreationReplacementOptionalReviewIsExact(t *testing.T) {
	base := `{"request_id":"11111111-1111-4111-8111-111111111111","name":"Replacement","secret":"transient","reason":"Reviewed"}`
	var input service.CredentialReplacementInput
	if json.Unmarshal([]byte(base), &input) != nil {
		t.Fatal("historical inline shape rejected")
	}
	for _, extra := range []string{`"storage_policy_etag":null`, `"storage_policy_etag":[]`, `"storage_policy_etag":""`, `"storage_policy_etag":"abc"`, `"storage_policy_etag":"` + strings.Repeat("a", 64) + `","storage_policy_etag":"` + strings.Repeat("b", 64) + `"`} {
		if json.Unmarshal([]byte(strings.TrimSuffix(base, "}")+","+extra+"}"), &input) == nil {
			t.Fatal("malformed optional review accepted")
		}
	}
}

func TestCredentialStoragePolicyStrongReviewBoundary(t *testing.T) {
	raw := strings.Repeat("a", 64)
	quoted := strconv.Quote(raw)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/policy", func(c *fox.Context) error {
		review, err := credentialStoragePolicyHeader(c)
		if err != nil {
			return err
		}
		if review != raw {
			c.Status(http.StatusUnprocessableEntity)
			return nil
		}
		c.Status(http.StatusNoContent)
		return nil
	})
	for _, tc := range []struct {
		name    string
		headers []string
		want    int
	}{
		{"current quoted GET token", []string{quoted}, 204},
		{"missing", nil, 400}, {"unquoted", []string{raw}, 400},
		{"weak", []string{"W/" + quoted}, 400}, {"wildcard", []string{"*"}, 400},
		{"uppercase", []string{strconv.Quote(strings.Repeat("A", 64))}, 400},
		{"connection identity token", []string{strconv.Quote(raw + "." + raw)}, 400},
		{"duplicate", []string{quoted, quoted}, 400},
		{"list", []string{quoted + "," + quoted}, 400},
		{"short", []string{strconv.Quote(raw[:63])}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("PUT", "/policy", nil)
			for _, header := range tc.headers {
				request.Header.Add("If-Match", header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status %d want %d", response.Code, tc.want)
			}
		})
	}
}
