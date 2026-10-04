package service

import "testing"

func TestRuntimeDigestTracksPrivateCredentialCiphertext(t *testing.T) {
	s, data, _ := runtimeFixture(t, "http://127.0.0.1")
	t.Cleanup(s.upstream.CloseIdleConnections)
	before, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	metadata := credentialMetadataRecord(data.Credentials[0]).ETag
	ciphertext, err := s.secrets.Seal(data.Credentials[0].ID, "different-private-fixture")
	if err != nil {
		t.Fatal(err)
	}
	data.Credentials[0].Ciphertext = ciphertext
	if metadata != credentialMetadataRecord(data.Credentials[0]).ETag {
		t.Fatal("fixture changed metadata instead of only private ciphertext")
	}
	after, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("private ciphertext change retained the routing source digest")
	}
	repeated, err := runtimeDigest(data)
	if err != nil || repeated != after {
		t.Fatal("stable ciphertext caused a spurious publication", err)
	}
}

func TestRuntimeRoutesRetainExactPrivateCipherIdentity(t *testing.T) {
	s, data, _ := runtimeFixture(t, "http://127.0.0.1")
	t.Cleanup(s.upstream.CloseIdleConnections)
	old := s.runtime.routes.Load()
	original := personalHash(data.Credentials[0].Ciphertext)
	if old.Models["mdl_one"][0].Credentials[0].CipherHash != original {
		t.Fatal("prepared route lacks the original private cipher identity")
	}
	ciphertext, err := s.secrets.Seal(data.Credentials[0].ID, "different-private-fixture")
	if err != nil {
		t.Fatal(err)
	}
	data.Credentials[0].Ciphertext = ciphertext
	routes, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeRuntimeClients(routes) })
	fresh := routes["mdl_one"][0].Credentials[0]
	if fresh.CipherHash != personalHash(ciphertext) || fresh.CipherHash == original || fresh.Plaintext != "different-private-fixture" {
		t.Fatal("new routing failed to capture its exact authenticated ciphertext")
	}
	if old.Models["mdl_one"][0].Credentials[0].CipherHash != original || old.Models["mdl_one"][0].Credentials[0].Plaintext != "test-upstream-secret" {
		t.Fatal("new preparation rewrote the immutable old snapshot")
	}
}
