package oauth

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

func endpoint(raw string) (*url.URL, error) {
	if !text(raw, MaxURLBytes) || strings.ContainsAny(raw, " \\#") {
		return nil, ErrConfig
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.Hostname() == "" ||
		target.User != nil || target.Fragment != "" || target.RawFragment != "" || target.Opaque != "" ||
		target.RawQuery != "" || target.ForceQuery {
		return nil, ErrConfig
	}
	return target, nil
}

func policy(ctx context.Context, config Config, target *url.URL) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	// The policy gets a private copy and cannot rewrite the actual request URL.
	copyURL := *target
	err := config.EndpointPolicy(ctx, &copyURL)
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	if err != nil {
		return ErrPolicy
	}
	return nil
}

func admitAll(ctx context.Context, config Config) error {
	for _, raw := range []string{config.AuthorizationURL, config.TokenURL, config.UserInfoURL, config.RedirectURL} {
		target, err := endpoint(raw)
		if err != nil {
			return ErrConfig
		}
		if err := policy(ctx, config, target); err != nil {
			return err
		}
	}
	return nil
}

func response(ctx context.Context, config Config, request *http.Request, bound int64) ([]byte, error) {
	target, err := endpoint(request.URL.String())
	if err != nil {
		return nil, ErrPolicy
	}
	if err := policy(ctx, config, target); err != nil {
		return nil, err
	}
	// Calling only the injected non-replaying RoundTripper avoids http.Client
	// redirect, cookie-jar and authentication negotiation behavior entirely.
	reply, err := config.Transport.RoundTrip(request)
	if err != nil {
		if reply != nil && reply.Body != nil {
			_ = reply.Body.Close()
		}
		return nil, ErrUnavailable
	}
	if reply == nil || reply.Body == nil {
		return nil, ErrUnavailable
	}
	data, readErr := io.ReadAll(io.LimitReader(reply.Body, bound+1))
	closeErr := reply.Body.Close()
	if ctx.Err() != nil || readErr != nil || closeErr != nil || int64(len(data)) > bound {
		clear(data)
		return nil, ErrUnavailable
	}
	mediaType, _, mediaErr := mime.ParseMediaType(reply.Header.Get("Content-Type"))
	if reply.StatusCode != http.StatusOK || mediaErr != nil || mediaType != "application/json" {
		clear(data)
		return nil, ErrProtocol
	}
	return data, nil
}
