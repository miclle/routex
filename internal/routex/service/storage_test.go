package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
)

func TestStorageCredentialRevisionBinding(t *testing.T) {
	store, err := secretstore.New(bytes.Repeat([]byte{35}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{secrets: store}
	input := StorageInput{Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "routex-test", Prefix: "attachments", Auth: StorageAuthInput{Action: "replace", AccessKey: "test-only-access", SecretKey: "test-only-secret"}}
	row, err := svc.prepareStorage(entity.StorageRevision{}, "usr_test", input)
	if err != nil {
		t.Fatal(err)
	}
	config, err := svc.storageConfig(row)
	if err != nil || config.SecretKey != "test-only-secret" {
		t.Fatal("encrypted credentials did not roundtrip")
	}
	encoded, _ := json.Marshal(storageRevisionView(row))
	if strings.Contains(string(encoded), "test-only") || strings.Contains(row.AuthCiphertext, "test-only") {
		t.Fatal("credentials leaked")
	}
	tampered := row
	tampered.ID = "str_other"
	if _, err := svc.storageConfig(tampered); err == nil {
		t.Fatal("revision swap accepted")
	}
	input.Auth = StorageAuthInput{Action: "keep"}
	next, err := svc.prepareStorage(row, "usr_test", input)
	if err != nil || next.AuthCiphertext == row.AuthCiphertext {
		t.Fatal("kept credentials did not receive a new envelope")
	}
	input.Endpoint = "https://other.example.com"
	if _, err := svc.prepareStorage(row, "usr_test", input); err == nil {
		t.Fatal("stored credentials forwarded to changed host")
	}
	input.Auth = StorageAuthInput{Action: "remove"}
	input.Enabled = true
	if _, err := svc.prepareStorage(row, "usr_test", input); err == nil {
		t.Fatal("enabled anonymous storage accepted")
	}
	input.Enabled = false
	removed, err := svc.prepareStorage(row, "usr_test", input)
	if err != nil || removed.AuthCiphertext != "" {
		t.Fatal("explicit disabled credential removal failed")
	}
	noKey := &Service{}
	input.Auth = StorageAuthInput{Action: "replace", AccessKey: "test-only-access", SecretKey: "test-only-secret"}
	if _, err := noKey.prepareStorage(row, "usr_test", input); err == nil {
		t.Fatal("credentials stored without encryption key")
	}
}
