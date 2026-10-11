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

func TestTelegramWireDoesNotAdmitGitHubCallbackOrMutableProfile(t *testing.T) {
	good := TelegramProviderInput{Name: "Telegram sign-in", ClientID: "client-id", CallbackURL: "https://routex.example/api/v1/auth/telegram/callback", SecretAction: "replace", ClientSecret: "private-test-secret", Reason: "Configure sign-in"}
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var out TelegramProviderInput
	if e = json.Unmarshal(raw, &out); e != nil || out != good {
		t.Fatal("valid fixed wire", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), "/telegram/callback", "/github/callback", 1), strings.Replace(string(raw), "/telegram/callback", "/telegram/%63allback", 1), strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`, strings.TrimSuffix(string(raw), "}") + `,"profile_id":"github.com.oauth-app.v1"}`, strings.Replace(string(raw), `"client_id":"client-id"`, `"client_id":null`, 1), strings.Replace(string(raw), `"secret_action":"replace"`, `"secret_action":"keep"`, 1)} {
		if e = json.Unmarshal([]byte(bad), &out); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid Telegram config admitted", e)
		}
	}
	var github GitHubProviderInput
	if json.Unmarshal(raw, &github) == nil {
		t.Fatal("Telegram callback admitted as GitHub")
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
func TestTelegramFixedNamespaceDerivationAndRootCoverage(t *testing.T) {
	for _, tc := range []struct {
		provider, kind, value string
		ok                    bool
	}{
		{"telegram", "string", "CaseSensitive-1", true}, {"telegram", "string", "001", true}, {"telegram", "integer", "1", false}, {"telegram", "string", "", false}, {"telegram", "string", "é", false}, {"telegram", "string", "x\n", false}, {"telegram", "string", strings.Repeat("x", 256), false}, {"Telegram", "string", "1", false}, {"github", "integer", "1", true}, {"github", "string", "1", false},
	} {
		if namedIdentityProfileSubject(tc.provider, tc.kind, tc.value) != tc.ok {
			t.Fatal("typed namespace")
		}
	}
	if namedIdentitySubjectDigest("telegram", "string", "1") == namedIdentitySubjectDigest("github", "integer", "1") || namedIdentitySubjectDigest("telegram", "string", "User") == namedIdentitySubjectDigest("telegram", "string", "user") {
		t.Fatal("identity alias")
	}
	cookie := strings.Repeat("c", 43)
	cid := "nic_exact"
	legacy := sha256.Sum256([]byte("routex-named-identity:github.com.oauth-app.v1:pkce:" + cookie + ":" + cid))
	if namedIdentityDerived(cookie, cid, "pkce") != base64.RawURLEncoding.EncodeToString(legacy[:]) {
		t.Fatal("GitHub derivation changed")
	}
	pkce := namedIdentityDerivedFor("telegram", cookie, cid, "pkce")
	nonce := namedIdentityDerivedFor("telegram", cookie, cid, "nonce")
	if len(pkce) != 43 || len(nonce) != 43 || pkce == nonce || pkce == namedIdentityDerived(cookie, cid, "pkce") || nonce == namedIdentityDerivedFor("telegram", cookie, "nic_other", "nonce") {
		t.Fatal("ceremony/domain separation")
	}
	gen := strings.Repeat("a", 64)
	if rootReference("named_identity_providers", "github", gen) != "named-identity:github.com.oauth-app.v1:github:"+gen || rootReference("named_identity_providers", "telegram", gen) != "named-identity:telegram.oidc.v1:telegram:"+gen || rootReference("named_identity_providers", "Telegram", gen) != "" {
		t.Fatal("fixed AAD")
	}
	if rootInventoryVersion != 10 || len(rootDomains) != 12 || !reflect.DeepEqual(rootInventoryDomains(6), rootDomains[:11]) || !reflect.DeepEqual(rootInventoryDomains(7), rootDomains[:11]) || !reflect.DeepEqual(rootInventoryDomains(8), rootDomains[:11]) || !reflect.DeepEqual(rootInventoryDomains(9), rootDomains[:12]) {
		t.Fatal("V6/V7 history and V8 coverage")
	}
	now := time.Now().UTC()
	svc := &Service{rootNow: func() time.Time { return now }}
	job := entity.SecretRotationJob{InventoryVersion: 6, Domain: 11}
	proof := entity.SecretProcessVerification{InventoryVersion: 6}
	if svc.rootObservationEligible(job, proof) {
		t.Fatal("old GitHub-only observation relabeled Telegram-covered")
	}
	job.InventoryVersion, proof.InventoryVersion = 9, 9
	if svc.rootObservationEligible(job, proof) {
		t.Fatal("old pre-Telegram observation relabeled Telegram-covered")
	}
}
func TestTelegramRuntimeExactSevenProofAndIndependentTombstones(t *testing.T) {
	tx := authenticationProofPolicyDB(t)
	birth := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	config, policy := strings.Repeat("a", 64), strings.Repeat("b", 64)
	u := entity.User{ID: "usr_exact", CreatedAt: birth}
	p := entity.NamedIdentityProvider{ID: "telegram", ProfileID: telegramProfileID, IdentityIssuer: telegramIdentityIssuer, Enabled: true, ConfigRevision: config, PolicyRevision: policy, VerifiedConfigRevision: config, VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	b := entity.NamedIdentityBinding{ID: "nib_member", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, UserID: u.ID, UserCreatedAt: birth, CreatedAt: birth, ConfigRevision: config, SubjectKind: "string", Subject: "Exact", SubjectDigest: namedIdentitySubjectDigest(p.ID, "string", "Exact")}
	row := entity.Session{PrimaryMethod: "telegram", UserID: u.ID, NamedIdentityProviderID: p.ID, NamedIdentityProfileID: p.ProfileID, NamedIdentityBindingID: b.ID, NamedIdentityBindingCreatedAt: &birth, NamedIdentityConfigRevision: config, NamedIdentityPolicyRevision: policy, NamedIdentityUserCreatedAt: &birth}
	bindings := map[string]entity.NamedIdentityBinding{b.ID: b}
	users := map[string]entity.User{u.ID: u}
	if !namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("valid Telegram runtime")
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
				q.IdentityIssuer = "oauth.telegram.org"
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
	telegram := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKeyFor("telegram", policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKeyFor("telegram", b.ID, &birth)}
	github := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKey(policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKey(b.ID, &birth)}
	rt.invalidateRuntimeNamedIdentityPolicy("telegram", policy)
	rt.invalidateRuntimeNamedIdentityBinding("telegram", b.ID, birth)
	if !rt.runtimeNamedIdentitySessionDenied(telegram) || rt.runtimeNamedIdentitySessionDenied(github) {
		t.Fatal("profile fence collision")
	}
	if !errors.Is(primaryValidateSession(tx, entity.Session{PrimaryMethod: "telegram", NamedIdentityProviderID: "github", NamedIdentityProfileID: githubProfileID}), apperrors.ErrUnauthorized) {
		t.Fatal("method/profile downgrade")
	}
	if !errors.Is(primaryValidateChallenge(tx, entity.MFAChallenge{PrimaryMethod: "telegram"}), apperrors.ErrUnauthorized) {
		t.Fatal("incomplete native MFA")
	}
}
func TestTelegramProtocolFactoryIsFixedAndOperationLocal(t *testing.T) {
	store, e := secretstore.New([]byte(strings.Repeat("k", 32)))
	if e != nil {
		t.Fatal(e)
	}
	p := entity.NamedIdentityProvider{ID: "telegram", ProfileID: telegramProfileID, IdentityIssuer: telegramIdentityIssuer, ClientID: "client-id", CallbackURL: "https://routex.example/api/v1/auth/telegram/callback", SecretGeneration: strings.Repeat("a", 64)}
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
			if r.URL.String() != "https://oauth.telegram.org/token" || r.Method != "POST" {
				t.Fatal("fixed token endpoint")
			}
			raw, e := io.ReadAll(r.Body)
			if e != nil {
				t.Fatal(e)
			}
			_ = r.Body.Close()
			f, e := url.ParseQuery(string(raw))
			_, _, basic := r.BasicAuth()
			if e != nil || !basic || f.Get("client_secret") != "" || f.Get("code_verifier") == "" || f.Get("client_id") != p.ClientID {
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
	if e != nil || !strings.HasPrefix(raw, "https://oauth.telegram.org/auth?") || calls != 0 {
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
	rejected := telegramRequestTransport{next: githubRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe destination reached injected transport")
		return nil, nil
	})}
	for _, target := range []string{"http://oauth.telegram.org/token", "https://oauth.telegram.org:443/token", "https://evil.example/token", "https://oauth.telegram.org/.well-known/jwks.json?access_token=x", "https://oauth.telegram.org/auth"} {
		r, e := http.NewRequestWithContext(context.Background(), "POST", target, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = rejected.RoundTrip(r); e == nil {
			t.Fatal("endpoint bypass")
		}
	}
}
