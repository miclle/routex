package upstream

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
)

const maxCertificatePEMBytes = 64 << 10

var errCertificate = errors.New("invalid upstream certificate material")

// NewCertificateClient accepts bounded in-memory PEM, never paths or a caller
// tls.Config. It retains the guarded DNS/address/redirect policy and uses fresh
// HTTP/1 connections without replay. HTTPS and server hostname/chain verification
// are mandatory. Empty serverCA uses system trust; explicit CA replaces that trust
// and is independent from the client certificate's issuer.
//
// Caller buffers are neither retained nor modified. Parsed private-key objects
// necessarily live with this operation-scoped client; discard it after closing
// idle connections. Go cannot guarantee cryptographic erasure of those objects.
func NewCertificateClient(allowPrivate bool, certificatePEM, keyPEM, serverCAPEM []byte) (*http.Client, error) {
	if len(certificatePEM) == 0 || len(certificatePEM) > maxCertificatePEMBytes || len(keyPEM) == 0 || len(keyPEM) > maxCertificatePEMBytes || len(serverCAPEM) > maxCertificatePEMBytes {
		return nil, errCertificate
	}
	certificateBytes := append([]byte(nil), certificatePEM...)
	keyBytes := append([]byte(nil), keyPEM...)
	caBytes := append([]byte(nil), serverCAPEM...)
	defer clear(certificateBytes)
	defer clear(keyBytes)
	defer clear(caBytes)
	if !certificateBlocks(certificateBytes) || !privateKeyBlock(keyBytes) {
		return nil, errCertificate
	}
	certificate, err := tls.X509KeyPair(certificateBytes, keyBytes)
	if err != nil {
		return nil, errCertificate
	}
	var roots *x509.CertPool
	if len(caBytes) != 0 {
		roots = x509.NewCertPool()
		remaining := bytes.TrimSpace(caBytes)
		if len(remaining) == 0 {
			return nil, errCertificate
		}
		for len(remaining) != 0 {
			if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
				return nil, errCertificate
			}
			block, rest := firstPEMBlock(remaining)
			if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				return nil, errCertificate
			}
			ca, err := x509.ParseCertificate(block.Bytes)
			if err != nil || !ca.BasicConstraintsValid || !ca.IsCA {
				return nil, errCertificate
			}
			roots.AddCert(ca)
			remaining = bytes.TrimSpace(rest)
		}
	}
	client := NewNonReplayingClient(allowPrivate)
	transport := client.Transport.(*policyTransport)
	transport.httpsOnly = true
	transport.base.TLSClientConfig = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
	}
	return client, nil
}

func certificateBlocks(raw []byte) bool {
	remaining := bytes.TrimSpace(raw)
	for len(remaining) != 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return false
		}
		block, rest := firstPEMBlock(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return false
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return false
		}
		remaining = bytes.TrimSpace(rest)
	}
	return len(bytes.TrimSpace(raw)) != 0
}

func privateKeyBlock(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte("-----BEGIN ")) {
		return false
	}
	block, rest := firstPEMBlock(trimmed)
	if block == nil || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return false
	}
	return block.Type == "PRIVATE KEY" || block.Type == "RSA PRIVATE KEY" || block.Type == "EC PRIVATE KEY"
}

// firstPEMBlock rejects Decode's permissive skip-ahead behavior. The consumed
// span must start at this exact block and contain no skipped or nested BEGIN.
// Callers validate its type/headers and account for every remaining byte.
func firstPEMBlock(raw []byte) (*pem.Block, []byte) {
	trimmed := bytes.TrimSpace(raw)
	block, rest := pem.Decode(trimmed)
	if block == nil {
		return nil, nil
	}
	consumed := trimmed[:len(trimmed)-len(rest)]
	if !bytes.HasPrefix(consumed, []byte("-----BEGIN "+block.Type+"-----")) || bytes.Count(consumed, []byte("-----BEGIN ")) != 1 {
		return nil, nil
	}
	return block, rest
}
