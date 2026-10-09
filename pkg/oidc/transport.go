package oidc

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

func endpoint(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > MaxURLBytes || !boundedText(raw, MaxURLBytes) || strings.ContainsAny(raw, "\\#") {
		return nil, ErrConfig
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, ErrConfig
	}
	return parsed, nil
}

func admit(ctx context.Context, config Config, raw string, authorization bool) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	var parsed *url.URL
	var err error
	if authorization {
		if len(raw) > maxAuthorizationURLBytes {
			return ErrPolicy
		}
		parsed, err = url.Parse(raw)
		if err == nil {
			clean := *parsed
			clean.RawQuery = ""
			clean.ForceQuery = false
			_, err = endpoint(clean.String())
		}
	} else {
		parsed, err = endpoint(raw)
	}
	if err != nil {
		return ErrPolicy
	}
	// The policy gets a private URL copy; it cannot rewrite the dispatched target.
	copyURL := *parsed
	if config.EndpointPolicy(ctx, &copyURL) != nil {
		return ErrPolicy
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}

type requestBound struct {
	method string
	bytes  int64
}

type boundedTransport struct {
	config    Config
	endpoints map[string]requestBound
}

func operationClient(config Config, endpoints map[string]requestBound) *http.Client {
	return &http.Client{Transport: &boundedTransport{config: config, endpoints: endpoints},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func (t *boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	bound, ok := t.endpoints[request.URL.String()]
	if !ok || request.Method != bound.method {
		return nil, ErrPolicy
	}
	if err := admit(request.Context(), t.config, request.URL.String(), false); err != nil {
		return nil, err
	}
	response, err := t.config.Transport.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, ErrUnavailable
	}
	if response == nil || response.Body == nil {
		return nil, ErrUnavailable
	}
	// Buffer only bounded JSON, then close the real body exactly once. Library
	// parsers receive an in-memory body and cannot trigger a second network read.
	body, readErr := io.ReadAll(io.LimitReader(response.Body, bound.bytes+1))
	closeErr := response.Body.Close()
	if request.Context().Err() != nil || readErr != nil || closeErr != nil || int64(len(body)) > bound.bytes {
		return nil, ErrUnavailable
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaErr != nil || mediaType != "application/json" {
		return nil, ErrProtocol
	}
	copyResponse := *response
	copyResponse.Body = io.NopCloser(bytes.NewReader(body))
	copyResponse.ContentLength = int64(len(body))
	return &copyResponse, nil
}
