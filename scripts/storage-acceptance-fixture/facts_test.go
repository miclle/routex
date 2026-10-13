package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestExactRecordedVersionSurvivesReceiptRoundTrip(t *testing.T) {
	original := storageFact{StorageObject: entity.StorageObject{ID: "obj_fixture", OwnerKind: "user", OwnerID: "usr_fixture", VersionID: "private-production-field", SHA256: strings.Repeat("a", 64)}, RecordedVersionID: "recorded-version"}
	data, err := json.Marshal(factsInput{Objects: []storageFact{original}, RequestIDs: []string{"req_fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-production-field") {
		t.Fatal("production-hidden version was serialized")
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	read, err := readFactsInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Objects) != 1 || read.Objects[0].RecordedVersionID != "recorded-version" || read.Objects[0].ID != original.ID || read.Objects[0].SHA256 != original.SHA256 {
		t.Fatal("exact recorded receipt identity was lost")
	}
}

func TestFactsInputRejectsUnboundedAndAmbiguousTargets(t *testing.T) {
	cases := map[string]string{
		"duplicate_object":  `{"objects":[{"ID":"obj_test","OwnerID":"usr_test"},{"ID":"obj_test","OwnerID":"usr_test"}]}`,
		"unsafe_id":         `{"objects":[{"ID":"../object","OwnerID":"usr_test"}]}`,
		"unknown_field":     `{"objects":[],"unsafe":true}`,
		"trailing_value":    `{"objects":[]} {}`,
		"too_many_requests": `{"request_ids":["req_1","req_2","req_3","req_4","req_5","req_6","req_7","req_8","req_9"]}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readFactsInput(path); err == nil {
				t.Fatal("invalid target set accepted")
			}
		})
	}
}

func TestForeignDatabaseArgumentsAreRejectedBeforeOpening(t *testing.T) {
	valid := "postgres://routex_acceptance:test-password-only@127.0.0.1:62902/routex_acceptance?sslmode=disable"
	if err := validateFixtureDSN(valid); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{
		strings.Replace(valid, "/routex_acceptance?", "/routex?", 1),
		strings.Replace(valid, "127.0.0.1", "example.invalid", 1),
		strings.Replace(valid, "routex_acceptance:", "other:", 1),
		valid + "&host=example.invalid",
		valid + "&dbname=routex",
		valid + "&sslmode=disable",
	} {
		if err := validateFixtureDSN(dsn); err == nil {
			t.Fatal("foreign or ambiguous database accepted")
		}
	}
}

func TestForeignFixtureAndReceiptInputsCreateNoReceipt(t *testing.T) {
	t.Setenv("ROUTEX_ACCEPTANCE_ACCESS_KEY", "fixture-test-only-access")
	t.Setenv("ROUTEX_ACCEPTANCE_SECRET_KEY", strings.Repeat("a", 32))
	dir := t.TempDir()
	output := filepath.Join(dir, "receipt.json")
	for _, input := range []struct{ endpoint, bucket string }{
		{"https://example.invalid", "routex-acceptance-0123456789abcdef01234567"},
		{"http://127.0.0.1:1", "routex"},
		{"http://127.0.0.1:1?redirect=remote", "routex-acceptance-0123456789abcdef01234567"},
	} {
		if err := run(input.endpoint, input.bucket, output); err == nil {
			t.Fatal("foreign fixture admitted")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("foreign fixture created receipt")
		}
	}
	if err := validatePrivateReceipt("relative.json"); err == nil {
		t.Fatal("relative receipt accepted")
	}
	public := filepath.Join(dir, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateReceipt(filepath.Join(public, "receipt.json")); err == nil {
		t.Fatal("public receipt parent accepted")
	}
}
