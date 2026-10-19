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
	if issuer == nil {
		cert.IsCA = true
		cert.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	} else {
		parent, signer = issuer.parsed, issuer.signer
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		if server {
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			cert.DNSNames = []string{"localhost"}
		}
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
	files, err := fixturePKI(run, now)
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
