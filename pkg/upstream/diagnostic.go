package upstream

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type stageObserver func(string, time.Duration, error)

func (observe stageObserver) run(stage string, action func() error) error {
	start := time.Now()
	err := action()
	if observe != nil {
		observe(stage, time.Since(start), err)
	}
	return err
}

// Diagnostic contains measured transport facts, never raw peer responses or errors.
type Diagnostic struct {
	TransportOK bool              `json:"transport_ok"`
	APIOK       bool              `json:"api_ok"`
	HTTPStatus  int               `json:"http_status,omitempty"`
	DurationMS  int64             `json:"duration_ms"`
	Stages      []DiagnosticStage `json:"stages"`
}
type DiagnosticStage struct {
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Code       string `json:"code,omitempty"`
}

// Diagnose uses a fresh connection and the caller's explicit request. A non-2xx
// response can prove transport reachability, but is never called API success.
func Diagnose(ctx context.Context, request *http.Request, targetPrivate, proxyPrivate bool, config *EgressConfig) (Diagnostic, error) {
	if request == nil || request.URL == nil {
		return Diagnostic{}, errURL
	}
	if _, err := ValidateBaseURL(request.URL.String(), targetPrivate); err != nil {
		return Diagnostic{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := Diagnostic{Stages: []DiagnosticStage{}}
	stages := []string{"target_dns", "proxy_dns", "tcp", "proxy_tls", "proxy_auth", "proxy_connect", "target_tls", "api"}
	for _, stage := range stages {
		result.Stages = append(result.Stages, DiagnosticStage{Stage: stage, Status: "skipped"})
	}
	var mu sync.Mutex
	markNA := func(stage string) {
		for i := range result.Stages {
			if result.Stages[i].Stage == stage {
				result.Stages[i].Status = "not_applicable"
			}
		}
	}
	if _, literal := parseIP(request.URL.Hostname()); literal {
		markNA("target_dns")
	}
	if request.URL.Scheme != "https" {
		markNA("target_tls")
	}
	if config == nil {
		for _, stage := range []string{"proxy_dns", "proxy_tls", "proxy_auth", "proxy_connect"} {
			markNA(stage)
		}
	} else {
		if _, literal := parseIP(config.Host); literal {
			markNA("proxy_dns")
		}
		if config.Kind != "https" {
			markNA("proxy_tls")
		}
		if config.Username == "" {
			markNA("proxy_auth")
		}
	}
	observe := stageObserver(func(stage string, elapsed time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		for i := range result.Stages {
			item := &result.Stages[i]
			if item.Stage != stage || item.Status == "not_applicable" {
				continue
			}
			ms := elapsed.Milliseconds()
			if elapsed >= 0 {
				item.DurationMS = &ms
			}
			item.Status = "passed"
			if err != nil {
				item.Status = "failed"
				item.Code = "connection_failed"
				if ctx.Err() != nil {
					item.Code = "canceled_or_timed_out"
				}
			}
		}
	})
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var client *http.Client
	var err error
	if config != nil {
		client, err = newEgressClient(targetPrivate, proxyPrivate, *config, net.DefaultResolver.LookupNetIP, dialer.DialContext, nil, observe)
	} else {
		client = newClient(targetPrivate, net.DefaultResolver.LookupNetIP, dialer.DialContext)
		client.Transport.(*policyTransport).base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errAddress
			}
			var addresses []string
			err = observe.run("target_dns", func() error {
				ips, lookupErr := resolveEgressAddress(ctx, host, targetPrivate, net.DefaultResolver.LookupNetIP)
				for _, ip := range ips {
					addresses = append(addresses, net.JoinHostPort(ip.String(), port))
				}
				return lookupErr
			})
			if err != nil {
				return nil, err
			}
			var conn net.Conn
			err = observe.run("tcp", func() error {
				for _, address := range addresses {
					conn, err = dialer.DialContext(ctx, network, address)
					if err == nil {
						return nil
					}
				}
				return errAddress
			})
			return conn, err
		}
	}
	if err != nil {
		return result, err
	}
	defer client.CloseIdleConnections()
	var tlsStarted, apiStarted time.Time
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { mu.Lock(); tlsStarted = time.Now(); mu.Unlock() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mu.Lock()
			started := tlsStarted
			mu.Unlock()
			observe("target_tls", time.Since(started), err)
		},
		GotConn: func(httptrace.GotConnInfo) { mu.Lock(); apiStarted = time.Now(); mu.Unlock() },
	}
	req := request.Clone(httptrace.WithClientTrace(ctx, trace))
	// A diagnostic cannot inherit a caller-supplied Host or transport credentials.
	req.Host = ""
	req.Header.Del("Proxy-Authorization")
	start := time.Now()
	response, callErr := client.Do(req)
	if callErr == nil {
		_, readErr := io.CopyN(io.Discard, response.Body, 4096)
		_ = response.Body.Close()
		mu.Lock()
		elapsed := time.Since(apiStarted)
		mu.Unlock()
		observe("api", elapsed, nil)
		mu.Lock()
		result.TransportOK = true
		result.HTTPStatus = response.StatusCode
		result.APIOK = response.StatusCode >= 200 && response.StatusCode < 300 && (readErr == nil || readErr == io.EOF)
		if !result.APIOK {
			for i := range result.Stages {
				if result.Stages[i].Stage == "api" {
					result.Stages[i].Status = "failed"
					result.Stages[i].Code = "http_status"
					if readErr != nil && readErr != io.EOF {
						result.Stages[i].Code = "response_incomplete"
					}
				}
			}
		}
		mu.Unlock()
	} else {
		// A transport failure does not fabricate a target API measurement.
		mu.Lock()
		for i := range result.Stages {
			if result.Stages[i].Stage == "api" {
				result.Stages[i].Code = "not_reached"
			}
		}
		mu.Unlock()
	}
	mu.Lock()
	result.DurationMS = time.Since(start).Milliseconds()
	// Cancellation may leave a final transport callback in flight. Return an
	// independent slice so that callback cannot mutate the caller's result.
	final := result
	final.Stages = append([]DiagnosticStage(nil), result.Stages...)
	mu.Unlock()
	return final, nil
}
