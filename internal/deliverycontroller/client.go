package deliverycontroller

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
)

const machinePath = "/api/v1/api-key-delivery/controller"

type machineClient struct {
	client *http.Client
	cfg    Config
	bearer string
}

func (*machineClient) String() string     { return "RouteX machine client (redacted)" }
func (c *machineClient) GoString() string { return c.String() }

func newMachineClient(cfg Config) (*machineClient, error) {
	if cfg.validate() != nil {
		return nil, errors.New("invalid controller configuration")
	}
	raw, err := readProtected(cfg.BearerFile, 51)
	if err != nil {
		return nil, errors.New("read machine bearer failed")
	}
	defer clear(raw)
	bearer := strings.TrimSuffix(string(raw), "\n")
	if !strings.HasPrefix(bearer, "rxdctl_") || len(bearer) != 50 {
		return nil, errors.New("invalid machine bearer")
	}
	secret, err := base64.RawURLEncoding.DecodeString(bearer[7:])
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != bearer[7:] {
		clear(secret)
		return nil, errors.New("invalid machine bearer")
	}
	clear(secret)
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 8 << 10,
		DisableKeepAlives:      true,
		DisableCompression:     true,
	}
	return &machineClient{
		cfg: cfg, bearer: bearer,
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *machineClient) pull(ctx context.Context) (configuration, error) {
	var result configuration
	response, raw, err := c.request(ctx, http.MethodGet, machinePath+"/configuration", nil)
	if err != nil {
		return result, err
	}
	if decodeJSON(raw, &result) != nil || len(response.Header.Values("ETag")) != 1 || response.Header.Get("ETag") != "\""+result.ConfigurationDigest+"\"" || result.validate(c.cfg) != nil || ctx.Err() != nil {
		return configuration{}, errors.New("pulled configuration rejected")
	}
	return result, nil
}

type acknowledgement struct {
	RequestID           string `json:"request_id"`
	DescriptorRevision  string `json:"descriptor_revision"`
	ConfigurationDigest string `json:"configuration_digest"`
	AgentState          string `json:"agent_state"`
	OutputState         string `json:"output_state"`
}

type acknowledgementReceipt struct {
	acknowledgement
	RecordedAt   time.Time `json:"recorded_at"`
	EvidenceKind string    `json:"evidence_kind"`
}

func (c *machineClient) report(ctx context.Context, intent acknowledgement) error {
	if c.cfg.validate() != nil || !uuidPattern.MatchString(intent.RequestID) || intent.DescriptorRevision != c.cfg.DescriptorRevision || intent.ConfigurationDigest != c.cfg.ConfigurationDigest || intent.AgentState != "unknown" && intent.AgentState != "failed" || intent.OutputState != "unknown" && intent.OutputState != "written" && intent.OutputState != "failed" || ctx.Err() != nil {
		return errors.New("report budget or pinned intent is invalid")
	}
	body, err := json.Marshal(intent)
	if err != nil {
		return errors.New("encode controller report failed")
	}
	_, raw, err := c.request(ctx, http.MethodPost, machinePath+"/acknowledgements", body)
	if err != nil {
		return errors.New("controller report is uncertain; retain exact intent")
	}
	var receipt acknowledgementReceipt
	if decodeJSON(raw, &receipt) != nil || receipt.acknowledgement != intent || receipt.EvidenceKind != "controller_report" || !recordedAtValid(receipt.RecordedAt) || ctx.Err() != nil {
		return errors.New("controller report reply rejected; retain exact intent")
	}
	return nil
}

func recordedAtValid(value time.Time) bool {
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0 && value.Nanosecond()%1000 == 0 && value.Year() >= 1 && value.Year() <= 9999
}

func (c *machineClient) request(ctx context.Context, method, path string, body []byte) (_ *http.Response, _ []byte, runErr error) {
	// Wrapping the reader prevents implicit GetBody/replay; POST has no retry or
	// idempotency header. A retry is a separate explicit report-only command.
	var reader io.Reader
	if body != nil {
		reader = struct{ io.Reader }{bytes.NewReader(body)}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.RouteXURL+path, reader)
	if err != nil {
		return nil, nil, errors.New("create machine request failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(req)
	if err != nil {
		// Raw transport errors can contain URLs, header details or remote values.
		return nil, nil, errors.New("machine request failed")
	}
	defer func() {
		if response.Body.Close() != nil && runErr == nil {
			runErr = errors.New("close machine response failed")
		}
	}()
	media, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || parseErr != nil || media != "application/json" || len(response.Header.Values("Content-Type")) != 1 || len(response.Header.Values("Cache-Control")) != 1 || response.Header.Get("Cache-Control") != "private, no-store" || len(response.Header.Values("X-Content-Type-Options")) != 1 || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.ContentLength > maxJSONBytes || response.Header.Get("Content-Encoding") != "" {
		return nil, nil, errors.New("machine response rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxJSONBytes || ctx.Err() != nil {
		return nil, nil, errors.New("machine response incomplete or oversized")
	}
	return response, raw, nil
}
