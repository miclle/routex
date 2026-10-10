package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secretstore"
)

func TestGoogleWireDoesNotAdmitGitHubCallbackOrMutableProfile(t *testing.T) {
	good := GoogleProviderInput{Name: "Google sign-in", ClientID: "client-id", CallbackURL: "https://routex.example/api/v1/auth/google/callback", SecretAction: "replace", ClientSecret: "private-test-secret", Reason: "Configure sign-in"}
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var out GoogleProviderInput
	if e = json.Unmarshal(raw, &out); e != nil || out != good {
		t.Fatal("valid fixed wire", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), "/google/callback", "/github/callback", 1), strings.Replace(string(raw), "/google/callback", "/google/%63allback", 1), strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`, strings.TrimSuffix(string(raw), "}") + `,"profile_id":"github.com.oauth-app.v1"}`, strings.Replace(string(raw), `"client_id":"client-id"`, `"client_id":null`, 1), strings.Replace(string(raw), `"secret_action":"replace"`, `"secret_action":"keep"`, 1)} {
		if e = json.Unmarshal([]byte(bad), &out); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid Google config admitted", e)
		}
	}
	var github GitHubProviderInput
	if json.Unmarshal(raw, &github) == nil {
		t.Fatal("Google callback admitted as GitHub")
	}
	good.SecretAction = "keep"
	good.ClientSecret = ""
	raw, e = json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(raw, &out) != nil {
		t.Fatal("keep unavailable")
	}
}
func TestGoogleFixedNamespaceDerivationAndRootCoverage(t *testing.T) {
	for _, tc := range []struct {
		provider, kind, value string
		ok                    bool
	}{
		{"google", "string", "CaseSensitive-1", true}, {"google", "string", "001", true}, {"google", "integer", "1", false}, {"google", "string", "", false}, {"google", "string", "é", false}, {"google", "string", "x\n", false}, {"google", "string", strings.Repeat("x", 256), false}, {"Google", "string", "1", false}, {"github", "integer", "1", true}, {"github", "string", "1", false},
	} {
		if namedIdentityProfileSubject(tc.provider, tc.kind, tc.value) != tc.ok {
			t.Fatal("typed namespace")
		}
	}
	if namedIdentitySubjectDigest("google", "string", "1") == namedIdentitySubjectDigest("github", "integer", "1") || namedIdentitySubjectDigest("google", "string", "User") == namedIdentitySubjectDigest("google", "string", "user") {
		t.Fatal("identity alias")
	}
	cookie := strings.Repeat("c", 43)
	cid := "nic_exact"
	legacy := sha256.Sum256([]byte("routex-named-identity:github.com.oauth-app.v1:pkce:" + cookie + ":" + cid))
	if namedIdentityDerived(cookie, cid, "pkce") != base64.RawURLEncoding.EncodeToString(legacy[:]) {
		t.Fatal("GitHub derivation changed")
	}
	pkce := namedIdentityDerivedFor("google", cookie, cid, "pkce")
	nonce := namedIdentityDerivedFor("google", cookie, cid, "nonce")
	if len(pkce) != 43 || len(nonce) != 43 || pkce == nonce || pkce == namedIdentityDerived(cookie, cid, "pkce") || nonce == namedIdentityDerivedFor("google", cookie, "nic_other", "nonce") {
		t.Fatal("ceremony/domain separation")
	}
	gen := strings.Repeat("a", 64)
	if rootReference("named_identity_providers", "github", gen) != "named-identity:github.com.oauth-app.v1:github:"+gen || rootReference("named_identity_providers", "google", gen) != "named-identity:google.oidc.v1:google:"+gen || rootReference("named_identity_providers", "Google", gen) != "" {
		t.Fatal("fixed AAD")
	}
	if rootInventoryVersion != 7 || len(rootDomains) != 11 || !reflect.DeepEqual(rootInventoryDomains(6), rootDomains) || !reflect.DeepEqual(rootInventoryDomains(7), rootDomains) {
		t.Fatal("V6 history/V7 coverage")
	}
	now := time.Now().UTC()
	svc := &Service{rootNow: func() time.Time { return now }}
	job := entity.SecretRotationJob{InventoryVersion: 6, Domain: 11}
	proof := entity.SecretProcessVerification{InventoryVersion: 6}
	if svc.rootObservationEligible(job, proof) {
		t.Fatal("old GitHub-only observation relabeled Google-covered")
	}
}
func TestGoogleRuntimeExactSevenProofAndIndependentTombstones(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	config, policy := strings.Repeat("a", 64), strings.Repeat("b", 64)
	u := entity.User{ID: "usr_exact", CreatedAt: birth}
	p := entity.NamedIdentityProvider{ID: "google", ProfileID: googleProfileID, IdentityIssuer: googleIdentityIssuer, Enabled: true, ConfigRevision: config, PolicyRevision: policy, VerifiedConfigRevision: config, VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	b := entity.NamedIdentityBinding{ID: "nib_member", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, UserID: u.ID, UserCreatedAt: birth, CreatedAt: birth, ConfigRevision: config, SubjectKind: "string", Subject: "Exact", SubjectDigest: namedIdentitySubjectDigest(p.ID, "string", "Exact")}
	row := entity.Session{PrimaryMethod: "google", UserID: u.ID, NamedIdentityProviderID: p.ID, NamedIdentityProfileID: p.ProfileID, NamedIdentityBindingID: b.ID, NamedIdentityBindingCreatedAt: &birth, NamedIdentityConfigRevision: config, NamedIdentityPolicyRevision: policy, NamedIdentityUserCreatedAt: &birth}
	bindings := map[string]entity.NamedIdentityBinding{b.ID: b}
	users := map[string]entity.User{u.ID: u}
	if !namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("valid Google runtime")
	}
	for _, mode := range []string{"method", "provider", "profile", "binding_birth", "user_birth", "config", "policy", "disabled", "issuer", "typed_subject", "mixed_binding"} {
		t.Run(mode, func(t *testing.T) {
			r, q, d := row, p, b
			later := birth.Add(time.Microsecond)
			switch mode {
			case "method":
				r.PrimaryMethod = "github"
			case "provider":
				r.NamedIdentityProviderID = "github"
			case "profile":
				r.NamedIdentityProfileID = githubProfileID
			case "binding_birth":
				r.NamedIdentityBindingCreatedAt = &later
			case "user_birth":
				r.NamedIdentityUserCreatedAt = &later
			case "config":
				r.NamedIdentityConfigRevision = strings.Repeat("c", 64)
			case "policy":
				r.NamedIdentityPolicyRevision = strings.Repeat("c", 64)
			case "disabled":
				q.Enabled = false
			case "issuer":
				q.IdentityIssuer = "accounts.google.com"
			case "typed_subject":
				d.SubjectKind = "integer"
			case "mixed_binding":
				d.ProviderID = "github"
				d.ProfileID = githubProfileID
				d.IdentityIssuer = githubIdentityIssuer
			}
			if namedIdentityRuntimePrimary(r, &q, map[string]entity.NamedIdentityBinding{d.ID: d}, users) {
				t.Fatal("obsolete/mixed proof admitted")
			}
		})
	}
	rt := &Service{runtime: &gatewayRuntime{}}
	google := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKeyFor("google", policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKeyFor("google", b.ID, &birth)}
	github := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKey(policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKey(b.ID, &birth)}
	rt.invalidateRuntimeNamedIdentityPolicy("google", policy)
	rt.invalidateRuntimeNamedIdentityBinding("google", b.ID, birth)
	if !rt.runtimeNamedIdentitySessionDenied(google) || rt.runtimeNamedIdentitySessionDenied(github) {
		t.Fatal("profile fence collision")
	}
	if !errors.Is(primaryValidateSession(nil, entity.Session{PrimaryMethod: "google", NamedIdentityProviderID: "github", NamedIdentityProfileID: githubProfileID}), apperrors.ErrUnauthorized) {
		t.Fatal("method/profile downgrade")
	}
	if !errors.Is(primaryValidateChallenge(nil, entity.MFAChallenge{PrimaryMethod: "google"}), apperrors.ErrUnauthorized) {
		t.Fatal("incomplete native MFA")
	}
}
func TestGoogleProtocolFactoryIsFixedAndOperationLocal(t *testing.T) {
	store, e := secretstore.New([]byte(strings.Repeat("k", 32)))
	if e != nil {
		t.Fatal(e)
	}
	p := entity.NamedIdentityProvider{ID: "google", ProfileID: googleProfileID, IdentityIssuer: googleIdentityIssuer, ClientID: "client-id", CallbackURL: "https://routex.example/api/v1/auth/google/callback", SecretGeneration: strings.Repeat("a", 64)}
	p.AuthCiphertext, e = store.Seal(rootReference("named_identity_providers", p.ID, p.SecretGeneration), "private-secret")
	if e != nil {
		t.Fatal(e)
	}
	svc := &Service{secrets: store}
	created, closed, calls, bodies := 0, 0, 0, 0
	WithNamedIdentityTransportFactory(func(context.Context) (NamedIdentityTransport, error) {
		created++
		return NamedIdentityTransport{RoundTripper: githubRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.String() != "https://oauth2.googleapis.com/token" || r.Method != "POST" {
				t.Fatal("fixed token endpoint")
			}
			raw, e := io.ReadAll(r.Body)
			if e != nil {
				t.Fatal(e)
			}
			_ = r.Body.Close()
			f, e := url.ParseQuery(string(raw))
			_, _, basic := r.BasicAuth()
			if e != nil || !basic || f.Get("client_secret") != "" || f.Get("code_verifier") == "" {
				t.Fatal("Basic/PKCE")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: githubObservedBody{Reader: strings.NewReader(`{"id_token":"first","id_token":"second"}`), closed: &bodies}}, nil
		}), Close: func() error { closed++; return nil }}, nil
	})(svc)
	c, close, e := svc.namedIdentityProfileProtocol(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := c.AuthorizationURL(context.Background(), namedIdentityAuthorization{State: strings.Repeat("s", 43), Nonce: strings.Repeat("n", 43), PKCEVerifier: strings.Repeat("v", 43)})
	if e != nil || !strings.HasPrefix(raw, "https://accounts.google.com/o/oauth2/v2/auth?") || calls != 0 {
		t.Fatal("authorization contract", e)
	}
	_, e = c.Exchange(context.Background(), namedIdentityCallback{Code: "code", State: strings.Repeat("s", 43), ExpectedState: strings.Repeat("s", 43), ExpectedNonce: strings.Repeat("n", 43), PKCEVerifier: strings.Repeat("v", 43)})
	if e == nil || calls != 1 || bodies != 1 {
		t.Fatal("duplicate token failed open or leaked body")
	}
	firstCloseErr := close()
	secondCloseErr := close()
	if firstCloseErr != nil || secondCloseErr != nil || created != 1 || closed != 1 {
		t.Fatal("operation transport close")
	}
	if e = svc.ReceiveGoogleCallback(context.Background(), strings.Repeat("s", 43), strings.Repeat("s", 43), "code", "", "accounts.google.com"); !errors.Is(e, apperrors.ErrUnauthorized) || created != 1 {
		t.Fatal("callback issuer not rejected before database/remote I/O")
	}
	rejected := googleRequestTransport{next: githubRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe destination reached injected transport")
		return nil, nil
	})}
	for _, target := range []string{"http://oauth2.googleapis.com/token", "https://oauth2.googleapis.com:443/token", "https://evil.example/token", "https://www.googleapis.com/oauth2/v3/certs?access_token=x", "https://accounts.google.com/o/oauth2/v2/auth"} {
		r, e := http.NewRequestWithContext(context.Background(), "POST", target, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = rejected.RoundTrip(r); e == nil {
			t.Fatal("endpoint bypass")
		}
	}
}
