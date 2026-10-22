package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var errFixture = errors.New("certificate fixture refused")
var runIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type certificateIdentity struct {
	certificate []byte
	key         []byte
	parsed      *x509.Certificate
	signer      *ecdsa.PrivateKey
}

func (certificateIdentity) String() string   { return "certificate fixture identity (redacted)" }
func (certificateIdentity) GoString() string { return "certificate fixture identity (redacted)" }

func issueCertificate(name string, now time.Time, issuer *certificateIdentity, server bool) (certificateIdentity, error) {
	return issueIdentity(name, now, issuer, certificateProfile{server: server})
}

type certificateProfile struct {
	server, intermediate bool
	uri                  *url.URL
}

func issueIdentity(name string, now time.Time, issuer *certificateIdentity, profile certificateProfile) (certificateIdentity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certificateIdentity{}, errFixture
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil || serial.Sign() == 0 {
		return certificateIdentity{}, errFixture
	}
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	parent, signer := cert, key
	if issuer == nil || profile.intermediate {
		cert.IsCA = true
		cert.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	if issuer != nil {
		parent, signer = issuer.parsed, issuer.signer
		if !profile.intermediate {
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		if profile.server {
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			cert.DNSNames = []string{"localhost"}
		}
	}
	if profile.uri != nil {
		cert.URIs = []*url.URL{profile.uri}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, signer)
	if err != nil {
		return certificateIdentity{}, errFixture
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return certificateIdentity{}, errFixture
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return certificateIdentity{}, errFixture
	}
	return certificateIdentity{
		certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:         pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}),
		parsed:      parsed, signer: key,
	}, nil
}

// Server trust and client identity have independent issuers. No host trust store
// is modified, and the generated issuer private keys never leave this process.
func fixturePKI(run string, now time.Time) (map[string][]byte, error) {
	if !runIDPattern.MatchString(run) {
		return nil, errFixture
	}
	serverCA, err := issueCertificate("routex-server-ca-"+run, now, nil, false)
	if err != nil {
		return nil, err
	}
	clientCA, err := issueCertificate("routex-client-ca-"+run, now, nil, false)
	if err != nil {
		return nil, err
	}
	server, err := issueCertificate("localhost", now, &serverCA, true)
	if err != nil {
		return nil, err
	}
	writer, err := issueCertificate("routex-writer-"+run, now, &clientCA, false)
	if err != nil {
		return nil, err
	}
	reader, err := issueCertificate("routex-reader-"+run, now, &clientCA, false)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		"server-ca.pem": serverCA.certificate, "client-ca.pem": clientCA.certificate,
		"server.pem": server.certificate, "server.key": server.key,
		"writer.pem": writer.certificate, "writer.key": writer.key,
		"reader.pem": reader.certificate, "reader.key": reader.key,
	}, nil
}

func workloadURI(run, role string) string {
	return "spiffe://routex-" + run + ".invalid/vault/" + role
}

// This controlled X.509 fixture exercises cert-auth URI constraints. It is not
// a SPIRE deployment or a Vault SPIFFE auth backend. Issuer keys remain local.
func workloadPKI(run string, now time.Time) (map[string][]byte, error) {
	if !runIDPattern.MatchString(run) {
		return nil, errFixture
	}
	files := make(map[string][]byte)
	for _, side := range []string{"server", "client"} {
		root, err := issueCertificate("routex-"+side+"-root-"+run, now, nil, false)
		if err != nil {
			return nil, err
		}
		intermediate, err := issueIdentity("routex-"+side+"-intermediate-"+run, now, &root, certificateProfile{intermediate: true})
		if err != nil {
			return nil, err
		}
		files[side+"-ca.pem"] = root.certificate
		files[side+"-intermediate.pem"] = intermediate.certificate
		roles := []string{"writer", "reader", "outsider", "cnless"}
		if side == "server" {
			roles = []string{"server"}
		}
		for _, role := range roles {
			profile := certificateProfile{server: side == "server"}
			name := "localhost"
			if side == "client" {
				name = "routex-" + role + "-" + run
				uri := workloadURI(run, role)
				if role == "outsider" {
					// Matching the writer CN cannot substitute for its URI constraint.
					name = "routex-writer-" + run
					uri = "spiffe://other-" + run + ".invalid/vault/writer"
				} else if role == "cnless" {
					name = ""
					uri = workloadURI(run, "writer")
				}
				profile.uri, err = url.Parse(uri)
				if err != nil {
					return nil, errFixture
				}
			}
			leaf, err := issueIdentity(name, now, &intermediate, profile)
			if err != nil {
				return nil, err
			}
			files[role+".pem"] = append(append([]byte(nil), leaf.certificate...), intermediate.certificate...)
			files[role+".key"] = leaf.key
		}
	}
	return files, nil
}

func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errFixture
	}
	n, written := f.Write(data)
	synced := f.Sync()
	closed := f.Close()
	if n != len(data) || written != nil || synced != nil || closed != nil {
		return errFixture
	}
	return nil
}

func preparePKI(dir, run string, now time.Time) error {
	return prepareMaterials(dir, run, now, fixturePKI)
}

func prepareMaterials(dir, run string, now time.Time, generate func(string, time.Time) (map[string][]byte, error)) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || filepath.Base(dir) != "input" || filepath.Base(filepath.Dir(dir)) != "routex-vault-certificate-"+run {
		return errFixture
	}
	parent := filepath.Dir(dir)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errFixture
	}
	real, err := filepath.EvalSymlinks(parent)
	if err != nil || real != parent {
		return errFixture
	}
	files, err := generate(run, now)
	if err != nil || os.Mkdir(dir, 0700) != nil {
		return errFixture
	}
	manifest := make(map[string]string, len(files))
	for name, data := range files {
		if err = writePrivate(filepath.Join(dir, name), data); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		manifest[name] = hex.EncodeToString(sum[:])
	}
	raw, err := json.Marshal(struct {
		Run   string            `json:"run"`
		Files map[string]string `json:"files"`
	}{run, manifest})
	if err != nil {
		return errFixture
	}
	return writePrivate(filepath.Join(dir, "pki-manifest.json"), raw)
}
