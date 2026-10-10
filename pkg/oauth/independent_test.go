package oauth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/oauth"
)

const (
	independentTokenURL   = "https://identity.example.invalid/token"
	independentProfileURL = "https://identity.example.invalid/userinfo"
	independentState      = "ssssssssssssssssssssssssssssssss"
	independentVerifier   = "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"
	independentTokenJSON  = `{"access_token":"test-token","token_type":"Bearer"}`
)

type independentTransport func(*http.Request) (*http.Response, error)

func (f independentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	return f(r)
}

type independentBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *independentBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *independentBody) Close() error {
	b.closed = true
	return nil
}

func independentResponse(status int, text string) (*http.Response, *independentBody) {
	body := &independentBody{reader: strings.NewReader(text)}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       body,
	}, body
}

func independentConfig(transport http.RoundTripper) oauth.Config {
	return oauth.Config{
		AuthorizationURL: "https://identity.example.invalid/authorize",
		TokenURL:         independentTokenURL,
		UserInfoURL:      independentProfileURL,
		RedirectURL:      "https://routex.example.invalid/api/v1/auth/oauth/callback",
		ClientID:         "routex-test-client",
		ClientSecret:     "private-test-secret",
		ClientAuthMethod: oauth.ClientSecretBasic,
		Scopes:           []string{"profile", "email"},
		SubjectPath:      []string{"account", "id"},
		Transport:        transport,
		EndpointPolicy: func(ctx context.Context, target *url.URL) error {
			if target.Scheme != "https" {
				return errors.New("test destination rejected")
			}
			return ctx.Err()
		},
	}
}

func independentCallback() oauth.Callback {
	return oauth.Callback{
		Code:          "single-test-code",
		State:         independentState,
		ExpectedState: independentState,
		PKCEVerifier:  independentVerifier,
	}
}

func TestIndependentSubjectKindsPreserveExactLargeInteger(t *testing.T) {
	for _, test := range []struct {
		name, raw, kind, subject string
	}{
		{"integer_above_float_precision", `9007199254740993`, "integer", "9007199254740993"},
		{"same_text_string_is_distinct", `"9007199254740993"`, "string", "9007199254740993"},
		{"adjacent_integer_is_distinct", `9007199254740992`, "integer", "9007199254740992"},
		{"valid_surrogate_pair", `"\ud83d\ude80"`, "string", "🚀"},
		{"literal_replacement_character_is_valid", `"�"`, "string", "�"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var bodies []*independentBody
			cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw := independentTokenJSON
				if calls == 2 {
					if r.URL.String() != independentProfileURL || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
						t.Fatal("profile request lost exact endpoint or bearer authorization")
					}
					raw = `{"account":{"id":` + test.raw + `}}`
				} else if calls != 1 || r.URL.String() != independentTokenURL {
					t.Fatal("unexpected request or replay")
				}
				response, body := independentResponse(http.StatusOK, raw)
				bodies = append(bodies, body)
				return response, nil
			}))
			client, err := oauth.New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatal("constructor performed HTTP")
			}
			identity, err := client.Exchange(context.Background(), independentCallback())
			if err != nil || string(identity.Kind) != test.kind || identity.Subject != test.subject || calls != 2 {
				t.Fatalf("exact identity not preserved: kind=%q subject=%q calls=%d err=%v", identity.Kind, identity.Subject, calls, err)
			}
			for _, body := range bodies {
				if !body.closed {
					t.Fatal("successful response body remained open")
				}
			}
		})
	}
}

func TestIndependentAmbiguousJSONAndUnicodeCannotAliasIdentity(t *testing.T) {
	for _, test := range []struct {
		name, profile string
		path          []string
	}{
		{"escaped_duplicate_subject_key", `{"account":{"id":"first","\u0069d":"second"}}`, []string{"account", "id"}},
		{"escaped_duplicate_ancestor_key", `{"account":{"id":"first"},"\u0061ccount":{"id":"second"}}`, []string{"account", "id"}},
		{"duplicate_unselected_nested_key", `{"account":{"id":"actor","extra":{"x":1,"\u0078":2}}}`, []string{"account", "id"}},
		{"unpaired_high_surrogate_subject", `{"account":{"id":"\ud800"}}`, []string{"account", "id"}},
		{"unpaired_low_surrogate_subject", `{"account":{"id":"\udc00"}}`, []string{"account", "id"}},
		{"invalid_surrogate_pair_subject", `{"account":{"id":"\ud800\u0041"}}`, []string{"account", "id"}},
		{"surrogate_key_cannot_match_replacement_path", `{"account":{"\ud800":"alias"}}`, []string{"account", "�"}},
		{"invalid_utf8_subject", "{\"account\":{\"id\":\"" + string([]byte{0xff}) + "\"}}", []string{"account", "id"}},
		{"invalid_utf8_key_cannot_match_replacement_path", "{\"account\":{\"" + string([]byte{0xff}) + "\":\"alias\"}}", []string{"account", "�"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var bodies []*independentBody
			cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw := independentTokenJSON
				if calls == 2 {
					raw = test.profile
				} else if calls != 1 {
					t.Fatal("ambiguous identity caused replay")
				}
				response, body := independentResponse(http.StatusOK, raw)
				bodies = append(bodies, body)
				return response, nil
			}))
			cfg.SubjectPath = test.path
			client, err := oauth.New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := client.Exchange(context.Background(), independentCallback())
			if err == nil || identity.Subject != "" || calls != 2 {
				t.Fatal("malformed JSON became an authenticated identity")
			}
			for _, body := range bodies {
				if !body.closed {
					t.Fatal("rejected response body remained open")
				}
			}
		})
	}
	for _, segment := range []string{string([]byte{0xff}), string([]byte{0xed, 0xa0, 0x80})} {
		cfg := independentConfig(independentTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid configured path performed HTTP")
			return nil, errors.New("unexpected HTTP")
		}))
		cfg.SubjectPath = []string{"account", segment}
		if _, err := oauth.New(context.Background(), cfg); err == nil {
			t.Fatal("malformed UTF8 configured path accepted")
		}
	}
}

func TestIndependentBasicCredentialsAreFormEncodedBeforeBase64(t *testing.T) {
	calls := 0
	cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			// This fixed expected value encodes each credential before joining with a colon.
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Basic Y2xpZW50JTNBJTJCKyUyRiVDMyVBOTpzZWNyZXQlMjYlM0QlM0ElMjUrJTJC" {
				t.Fatal("reserved Basic credential characters were not form encoded")
			}
			raw, err := io.ReadAll(r.Body)
			closeErr := r.Body.Close()
			if err != nil || closeErr != nil {
				t.Fatal("read synthetic token request")
			}
			form, err := url.ParseQuery(string(raw))
			if err != nil || form.Get("client_id") != "" || form.Get("client_secret") != "" || form.Get("grant_type") != "authorization_code" || form.Get("code_verifier") != independentVerifier {
				t.Fatal("Basic authentication leaked credentials into form or changed PKCE")
			}
			response, _ := independentResponse(http.StatusOK, independentTokenJSON)
			return response, nil
		}
		if calls != 2 {
			t.Fatal("authentication fallback or replay")
		}
		response, _ := independentResponse(http.StatusOK, `{"account":{"id":"exact"}}`)
		return response, nil
	}))
	cfg.ClientID = "client:+ /é"
	cfg.ClientSecret = "secret&=:% +"
	client, err := oauth.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Exchange(context.Background(), independentCallback()); err != nil || calls != 2 {
		t.Fatal("explicit Basic exchange failed")
	}
}

func TestIndependentConfigSlicesCannotChangeCapturedAuthorizationOrSubject(t *testing.T) {
	calls := 0
	cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw := independentTokenJSON
		if calls == 2 {
			raw = `{"account":{"id":"captured"},"other":{"mutable":"wrong"}}`
		} else if calls != 1 {
			t.Fatal("unexpected replay")
		}
		response, _ := independentResponse(http.StatusOK, raw)
		return response, nil
	}))
	client, err := oauth.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Scopes[0] = "admin"
	cfg.SubjectPath[0], cfg.SubjectPath[1] = "other", "mutable"
	target, err := client.AuthorizationURL(context.Background(), oauth.Authorization{State: independentState, PKCEVerifier: independentVerifier})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Query().Get("scope") != "profile email" || parsed.Query().Get("state") != independentState || parsed.Query().Get("code_challenge_method") != "S256" || calls != 0 {
		t.Fatal("caller mutated captured scopes or authorization performed HTTP")
	}
	identity, err := client.Exchange(context.Background(), independentCallback())
	if err != nil || identity.Subject != "captured" || string(identity.Kind) != "string" || calls != 2 {
		t.Fatal("caller mutated captured subject path")
	}
}

func TestIndependentTokenRedirectAndUnauthorizedNeverFallbackOrReadProfile(t *testing.T) {
	for _, method := range []oauth.ClientAuthMethod{oauth.ClientSecretBasic, oauth.ClientSecretPost} {
		for _, status := range []int{http.StatusFound, http.StatusUnauthorized} {
			t.Run(string(method)+"_"+http.StatusText(status), func(t *testing.T) {
				calls := 0
				var body *independentBody
				cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls != 1 || r.URL.String() != independentTokenURL || r.Method != http.MethodPost {
						t.Fatal("rejected token request followed a redirect, retried or fetched profile")
					}
					response, captured := independentResponse(status, `{"error":"synthetic denial"}`)
					body = captured
					response.Header.Set("Location", "https://other.example.invalid/stolen")
					return response, nil
				}))
				cfg.ClientAuthMethod = method
				client, err := oauth.New(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				identity, err := client.Exchange(context.Background(), independentCallback())
				if err == nil || identity.Subject != "" || calls != 1 || body == nil || !body.closed {
					t.Fatal("token rejection did not fail closed and close its body")
				}
			})
		}
	}
}

func TestIndependentBoundedAndAmbiguousTokenResponsesCloseWithoutProfile(t *testing.T) {
	for _, test := range []struct {
		name, raw string
	}{
		{"oversized", strings.Repeat(" ", (64<<10)+4096)},
		{"escaped_duplicate_token", `{"access_token":"first","\u0061ccess_token":"second","token_type":"Bearer"}`},
		{"malformed_surrogate_token", `{"access_token":"\ud800","token_type":"Bearer"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var body *independentBody
			cfg := independentConfig(independentTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls != 1 {
					t.Fatal("invalid token response reached profile or retry")
				}
				response, captured := independentResponse(http.StatusOK, test.raw)
				body = captured
				return response, nil
			}))
			client, err := oauth.New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := client.Exchange(context.Background(), independentCallback())
			if err == nil || identity.Subject != "" || calls != 1 || body == nil || !body.closed || body.read > (64<<10)+1 {
				t.Fatal("invalid token response escaped admission or bounded closure")
			}
		})
	}
}

func TestIndependentOversizedProfileClosesAtOriginalByteBound(t *testing.T) {
	calls := 0
	var tokenBody, profileBody *independentBody
	cfg := independentConfig(independentTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			response, body := independentResponse(http.StatusOK, independentTokenJSON)
			tokenBody = body
			return response, nil
		}
		if calls != 2 {
			t.Fatal("oversized profile caused replay")
		}
		response, body := independentResponse(http.StatusOK, strings.Repeat(" ", (256<<10)+4096))
		profileBody = body
		return response, nil
	}))
	client, err := oauth.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.Exchange(context.Background(), independentCallback())
	if err == nil || identity.Subject != "" || calls != 2 || tokenBody == nil || !tokenBody.closed || profileBody == nil || !profileBody.closed || profileBody.read > (256<<10)+1 {
		t.Fatal("oversized profile escaped original bound or response closure")
	}
}

func TestIndependentParentCancellationAndDeadlineCoverBothExchangeStages(t *testing.T) {
	for _, stage := range []string{"token", "profile"} {
		t.Run(stage, func(t *testing.T) {
			parentBudget := 30 * time.Second
			if stage == "token" {
				parentBudget = 3 * time.Second
			}
			parent, cancel := context.WithTimeout(context.Background(), parentBudget)
			defer cancel()
			calls := 0
			var firstDeadline time.Time
			var tokenBody *independentBody
			cfg := independentConfig(independentTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				parentDeadline, parentOK := parent.Deadline()
				if !ok || !parentOK || deadline.After(parentDeadline) || time.Until(deadline) > 10*time.Second {
					t.Fatal("stage escaped parent or shared ten-second deadline")
				}
				if calls == 1 {
					firstDeadline = deadline
				} else if calls != 2 || !deadline.Equal(firstDeadline) {
					t.Fatal("profile renewed exchange budget or replayed")
				}
				if calls == 1 && stage == "profile" {
					response, body := independentResponse(http.StatusOK, independentTokenJSON)
					tokenBody = body
					return response, nil
				}
				cancel()
				select {
				case <-r.Context().Done():
				default:
					t.Fatal("parent cancellation did not reach active transport")
				}
				return nil, r.Context().Err()
			}))
			client, err := oauth.New(parent, cfg)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := client.Exchange(parent, independentCallback())
			wantCalls := 1
			if stage == "profile" {
				wantCalls = 2
			}
			if err == nil || identity.Subject != "" || calls != wantCalls || parent.Err() != context.Canceled {
				t.Fatal("canceled exchange continued or authenticated")
			}
			if tokenBody != nil && !tokenBody.closed {
				t.Fatal("token response remained open during profile cancellation")
			}
		})
	}
}
