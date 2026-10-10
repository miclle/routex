package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	oauthprotocol "github.com/miclle/routex/pkg/oauth"
	"github.com/miclle/routex/pkg/secretstore"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func githubValidInput() GitHubProviderInput {
	return GitHubProviderInput{Name: "GitHub sign-in", ClientID: "client-id", CallbackURL: "https://routex.example/api/v1/auth/github/callback", SecretAction: "replace", ClientSecret: "private-client-secret", Reason: "Configure sign-in"}
}
func TestGitHubStrictFixedProfileWire(t *testing.T) {
	good := githubValidInput()
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	var out GitHubProviderInput
	if err = json.Unmarshal(raw, &out); err != nil || out != good {
		t.Fatal("valid config", err)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"name":"GitHub sign-in"`, `"name":null`, 1), strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`, strings.TrimSuffix(string(raw), "}") + `,"authorization_url":"https://other.example"}`, strings.Replace(string(raw), "/github/callback", "/oauth/callback", 1), strings.Replace(string(raw), "/github/callback", "/github/%63allback", 1), strings.Replace(string(raw), `"secret_action":"replace"`, `"secret_action":"keep"`, 1)} {
		if err = json.Unmarshal([]byte(bad), &out); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("ambiguous or mutable endpoint wire admitted", err)
		}
	}
	for _, valid := range []string{`{"password":"a-valid-password","proof":{},"reason":"Link"}`, `{"password":"a-valid-password","proof":{"code":"123456"},"reason":"Link"}`, `{"password":"a-valid-password","proof":{"recovery_code":"recovery"},"reason":"Link"}`} {
		var v GitHubIdentityInput
		if err = json.Unmarshal([]byte(valid), &v); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{`{"password":"a-valid-password","proof":null,"reason":"Link"}`, `{"password":"a-valid-password","proof":{"code":"123456","recovery_code":"recovery"},"reason":"Link"}`, `{"password":"a-valid-password","proof":{"code":"123456","code":"654321"},"reason":"Link"}`, `{"password":"a-valid-password","proof":{},"reason":"Link","subject":"1"}`} {
		var v GitHubIdentityInput
		if err = json.Unmarshal([]byte(bad), &v); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("proof wire admitted", err)
		}
	}
	for _, bad := range []string{`{"enabled":null,"reason":"Enable"}`, `{"enabled":true,"reason":""}`, `{"enabled":true,"reason":"Enable","profile_id":"other"}`} {
		var v GitHubStatusInput
		if json.Unmarshal([]byte(bad), &v) == nil {
			t.Fatal("status wire")
		}
	}
}
func TestGitHubTypedNamespaceAndPrimaryNeverDowngrade(t *testing.T) {
	for _, tc := range []struct {
		kind, value string
		valid       bool
	}{{"integer", "0", true}, {"integer", "9007199254740993123456789", true}, {"string", "1", false}, {"integer", "01", false}, {"integer", "1e0", false}, {"integer", "1.0", false}, {"integer", "-1", false}, {"integer", "", false}} {
		if namedIdentitySubject(tc.kind, tc.value) != tc.valid {
			t.Fatal("typed subject", tc.kind, tc.value)
		}
	}
	if namedIdentitySubjectDigest("github", "integer", "1") == oauthSubjectDigest("oauth", "integer", "1") || namedIdentitySubjectDigest("github", "integer", "1") == namedIdentitySubjectDigest("other", "integer", "1") {
		t.Fatal("namespace aliases")
	}
	now := time.Now()
	for _, row := range []entity.Session{{PrimaryMethod: "GitHub"}, {PrimaryMethod: "github"}, {NamedIdentityProviderID: "github"}, {NamedIdentityProfileID: githubProfileID}, {NamedIdentityBindingID: "nib_old"}, {NamedIdentityBindingCreatedAt: &now}, {NamedIdentityConfigRevision: strings.Repeat("a", 64)}, {NamedIdentityPolicyRevision: strings.Repeat("b", 64)}, {NamedIdentityUserCreatedAt: &now}, {PrimaryMethod: "oidc", NamedIdentityProviderID: "github"}} {
		if !errors.Is(primaryValidateSession(nil, row), apperrors.ErrUnauthorized) {
			t.Fatal("partial proof downgraded")
		}
	}
	if primaryValidateSession(nil, entity.Session{}) != nil || primaryValidateChallenge(nil, entity.MFAChallenge{}) != nil {
		t.Fatal("local changed")
	}
	if !errors.Is(primaryValidateChallenge(nil, entity.MFAChallenge{PrimaryMethod: "github"}), apperrors.ErrUnauthorized) {
		t.Fatal("incomplete challenge admitted")
	}
	if namedIdentityError(errors.New("private token/profile/password")) != namedIdentityUnavailable || namedIdentityError(apperrors.ErrForbidden) != apperrors.ErrForbidden {
		t.Fatal("error privacy")
	}
}
func TestGitHubCeremonyFencesProfileBirthAndDeadline(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	birth := now.Add(-time.Minute)
	p := entity.NamedIdentityProvider{ID: githubProviderID, ProfileID: githubProfileID, IdentityIssuer: githubIdentityIssuer, CreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("a", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	c := entity.NamedIdentityCeremony{ID: "nic_first", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, ProviderCreatedAt: birth, Purpose: "login", CreatedAt: birth, ExpiresAt: birth.Add(namedIdentityCeremonyLifetime), ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if !namedIdentityCeremonyCurrent(p, c, now) {
		t.Fatal("valid ceremony")
	}
	for _, mode := range []string{"profile", "issuer", "provider_birth", "config", "policy", "disabled", "expired", "extended", "future", "purpose"} {
		t.Run(mode, func(t *testing.T) {
			q, d := p, c
			switch mode {
			case "profile":
				d.ProfileID = "other"
			case "issuer":
				d.IdentityIssuer = "https://GITHUB.com"
			case "provider_birth":
				q.CreatedAt = q.CreatedAt.Add(time.Microsecond)
			case "config":
				q.ConfigRevision = strings.Repeat("c", 64)
			case "policy":
				q.PolicyRevision = strings.Repeat("c", 64)
			case "disabled":
				q.Enabled = false
			case "expired":
				d.ExpiresAt = now
			case "extended":
				d.ExpiresAt = d.CreatedAt.Add(namedIdentityCeremonyLifetime + time.Microsecond)
			case "future":
				d.CreatedAt = now.Add(time.Second)
			case "purpose":
				d.Purpose = "unknown"
			}
			if namedIdentityCeremonyCurrent(q, d, now) {
				t.Fatal("obsolete proof")
			}
		})
	}
	p.Enabled = false
	c.Purpose = "verify"
	if !namedIdentityCeremonyCurrent(p, c, now) {
		t.Fatal("explicit admin verification blocked")
	}
	cookie := strings.Repeat("a", 43)
	if namedIdentityDerived(cookie, "nic_a", "pkce") == namedIdentityDerived(cookie, "nic_b", "pkce") || namedIdentityDerived(cookie, "nic_a", "pkce") == oauthDerived(cookie, "nic_a", "pkce") {
		t.Fatal("browser namespace missing")
	}
}

type githubRoundTripFunc func(*http.Request) (*http.Response, error)

func (f githubRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type githubObservedBody struct {
	io.Reader
	closed *int
}

func (b githubObservedBody) Close() error { (*b.closed)++; return nil }
func TestGitHubFixedRequestProfileAndOwnedTransport(t *testing.T) {
	store, err := secretstore.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	p := entity.NamedIdentityProvider{ID: githubProviderID, ProfileID: githubProfileID, IdentityIssuer: githubIdentityIssuer, ClientID: "client-id", CallbackURL: githubValidInput().CallbackURL, SecretGeneration: strings.Repeat("a", 64)}
	p.AuthCiphertext, err = store.Seal(rootReference("named_identity_providers", p.ID, p.SecretGeneration), "private-secret")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{secrets: store}
	calls, closed, bodies := 0, 0, 0
	var deadlines []time.Time
	WithNamedIdentityTransportFactory(func(ctx context.Context) (NamedIdentityTransport, error) {
		return NamedIdentityTransport{RoundTripper: githubRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := r.Context().Deadline()
			if !ok {
				t.Fatal("no operation deadline")
			}
			deadlines = append(deadlines, deadline)
			if r.Header.Get("User-Agent") != "RouteX" {
				t.Fatal("missing fixed User-Agent")
			}
			payload := ""
			switch calls {
			case 1:
				if r.Method != "POST" || r.URL.String() != "https://github.com/login/oauth/access_token" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "application/json" {
					t.Fatal("token request")
				}
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					t.Fatal(e)
				}
				f, e := url.ParseQuery(string(raw))
				if e != nil || len(f) != 6 || f.Get("grant_type") != "authorization_code" || f.Get("client_id") != p.ClientID || f.Get("client_secret") != "private-secret" || f.Get("code") != "code" || f.Get("redirect_uri") != p.CallbackURL || f.Get("code_verifier") != strings.Repeat("v", 43) {
					t.Fatal("exact token form")
				}
				payload = `{"access_token":"opaque-access-token","token_type":"Bearer"}`
			case 2:
				if r.Method != "GET" || r.URL.String() != "https://api.github.com/user" || r.Header.Get("Authorization") != "Bearer opaque-access-token" {
					t.Fatal("profile request")
				}
				payload = `{"id":9007199254740993123456789,"login":"mutable","email":"ignored"}`
			default:
				t.Fatal("retry or extra operation")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: githubObservedBody{strings.NewReader(payload), &bodies}}, nil
		}), Close: func() error { closed++; return nil }}, nil
	})(svc)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, closeClient, err := svc.namedIdentityProtocol(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := closeClient(); err != nil {
			t.Error(err)
		}
	}()
	auth, err := client.AuthorizationURL(ctx, oauthprotocol.Authorization{State: strings.Repeat("s", 43), PKCEVerifier: strings.Repeat("v", 43)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(auth)
	if err != nil || u.Host != "github.com" || u.Path != "/login/oauth/authorize" || len(u.Query()) != 6 || u.Query().Has("scope") {
		t.Fatal("fixed authorization request")
	}
	identity, err := client.Exchange(ctx, oauthprotocol.Callback{Code: "code", State: strings.Repeat("s", 43), ExpectedState: strings.Repeat("s", 43), PKCEVerifier: strings.Repeat("v", 43)})
	if err != nil || identity.Kind != oauthprotocol.SubjectInteger || identity.Subject != "9007199254740993123456789" {
		t.Fatal("exact typed GitHub proof", err)
	}
	if err = closeClient(); err != nil {
		t.Fatal(err)
	}
	if err = closeClient(); err != nil || calls != 2 || bodies != 2 || closed != 1 || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatal("owned closure/shared deadline", err, calls, bodies, closed)
	}
}
func TestGitHubInjectedTransportCannotChangeProfileEndpoints(t *testing.T) {
	calls := 0
	rt := namedIdentityRequestTransport{next: githubRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private transport diagnostic")
	})}
	for _, target := range []string{"http://github.com/login/oauth/access_token", "https://github.com.evil/login/oauth/access_token", "https://github.com:443/login/oauth/access_token", "https://api.github.com/user?access_token=private", "https://user:private@api.github.com/user", "https://github.com/login/oauth/authorize"} {
		r, err := http.NewRequestWithContext(context.Background(), "POST", target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = rt.RoundTrip(r); err == nil || calls != 0 {
			t.Fatal("injected endpoint bypass")
		}
	}
	if rootReference("named_identity_providers", "GitHub", "generation") != "" || rootReference("named_identity_providers", "github", "generation") != "named-identity:github.com.oauth-app.v1:github:generation" {
		t.Fatal("profile AAD")
	}
	if !reflect.DeepEqual(rootInventoryDomains(5), rootDomains[:10]) || rootInventoryVersion != 8 || rootDomains[10] != "named_identity_providers" {
		t.Fatal("versioned root domain")
	}
}
func TestGitHubRuntimeBirthAndIndependentTombstones(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	config, policy := strings.Repeat("a", 64), strings.Repeat("b", 64)
	u := entity.User{ID: "usr_member", CreatedAt: birth}
	p := entity.NamedIdentityProvider{ID: githubProviderID, ProfileID: githubProfileID, IdentityIssuer: githubIdentityIssuer, Enabled: true, ConfigRevision: config, PolicyRevision: policy, VerifiedConfigRevision: config, VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	b := entity.NamedIdentityBinding{ID: "nib_member", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, UserID: u.ID, UserCreatedAt: birth, CreatedAt: birth, ConfigRevision: config, SubjectKind: "integer", Subject: "1", SubjectDigest: namedIdentitySubjectDigest(p.ID, "integer", "1")}
	row := entity.Session{PrimaryMethod: "github", UserID: u.ID, NamedIdentityProviderID: p.ID, NamedIdentityProfileID: p.ProfileID, NamedIdentityBindingID: b.ID, NamedIdentityBindingCreatedAt: &birth, NamedIdentityConfigRevision: config, NamedIdentityPolicyRevision: policy, NamedIdentityUserCreatedAt: &birth}
	bindings := map[string]entity.NamedIdentityBinding{b.ID: b}
	users := map[string]entity.User{u.ID: u}
	if !namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("valid runtime proof")
	}
	changed := b
	changed.CreatedAt = changed.CreatedAt.Add(time.Microsecond)
	bindings[b.ID] = changed
	if namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("reborn binding")
	}
	bindings[b.ID] = b
	row.NamedIdentityProfileID = "other"
	if namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("wrong profile")
	}
	row.NamedIdentityProfileID = p.ProfileID
	rt := &Service{runtime: &gatewayRuntime{}}
	current := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKey(policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKey(b.ID, &birth)}
	if rt.runtimeNamedIdentitySessionDenied(current) {
		t.Fatal("new proof denied")
	}
	rt.invalidateRuntimeGitHubBinding(b.ID, birth)
	if !rt.runtimeNamedIdentitySessionDenied(current) {
		t.Fatal("unlink fence absent")
	}
	reborn := birth.Add(time.Microsecond)
	if rt.runtimeNamedIdentitySessionDenied(runtimeTeamSession{NamedIdentityBindingKey: namedIdentityRuntimeBindingKey(b.ID, &reborn)}) {
		t.Fatal("fence aliases replacement birth")
	}
	rt.invalidateRuntimeGitHubPolicy(policy)
	if !rt.runtimeNamedIdentitySessionDenied(runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKey(policy)}) || rt.runtimeOAuthSessionDenied(runtimeTeamSession{OAuthPolicyRevision: policy}) {
		t.Fatal("method policy fences not independent")
	}
}

func TestGitHubLocalDeadlineCannotRenewAfterRemoteWork(t *testing.T) {
	admitted := time.Now().Add(-6 * time.Second)
	outer, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	local, done := namedIdentityLocalContext(outer, admitted)
	defer done()
	deadline, ok := local.Deadline()
	if !ok || !deadline.Equal(admitted.Add(5*time.Second)) || !errors.Is(local.Err(), context.DeadlineExceeded) || outer.Err() != nil {
		t.Fatal("local budget renewed or remote cap conflated")
	}
	parent, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	local2, done2 := namedIdentityLocalContext(parent, time.Now())
	defer done2()
	a, _ := parent.Deadline()
	z, _ := local2.Deadline()
	if !a.Equal(z) || !errors.Is(local2.Err(), context.DeadlineExceeded) {
		t.Fatal("short parent not preserved")
	}
	canceled, stop3 := context.WithCancel(context.Background())
	local3, done3 := namedIdentityLocalContext(canceled, time.Now())
	defer done3()
	stop3()
	if !errors.Is(local3.Err(), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
