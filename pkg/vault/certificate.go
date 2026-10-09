package vault

import (
	"context"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

// NewCertificate creates an operation-scoped certificate-authenticated client
// from transient PEM. The server CA is not inferred from the client issuer.
// Close and discard the client after joining its requests; no token is retained,
// renewed or cached, and parsed Go private keys have no guaranteed memory erasure.
func NewCertificate(d Descriptor, allowPrivate bool, certificatePEM, keyPEM, serverCAPEM []byte) (*Client, error) {
	client, err := New(d, allowPrivate)
	if err != nil {
		return nil, err
	}
	client.Close()
	if client.endpoint.Scheme != "https" {
		return nil, &Failure{Stage: "prepare", Code: "invalid_descriptor"}
	}
	transport, err := upstream.NewCertificateClient(allowPrivate, certificatePEM, keyPEM, serverCAPEM)
	if err != nil {
		return nil, &Failure{Stage: "prepare", Code: "invalid_tls"}
	}
	transport.Timeout = requestTimeout
	client.http = transport
	client.certificateAuth = true
	return client, nil
}

// LoginCertificate makes one finite POST to the explicit auth mount with exactly
// the required named role. An empty/default role is never attempted. The client
// must originate from NewCertificate; this method performs no KV request.
func (c *Client) LoginCertificate(ctx context.Context, authMount, role string) (*LoginToken, Observation, error) {
	start := time.Now()
	if !c.certificateAuth || !validPath(authMount, 128, false) || !validSegment(role) {
		failure := &Failure{Stage: "prepare", Code: "invalid_auth"}
		return nil, Observation{Duration: time.Since(start), Failure: failure}, failure
	}
	return c.login(ctx, authMount, map[string]string{"name": role}, start)
}
