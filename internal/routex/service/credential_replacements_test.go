package service

import (
	"strings"
	"testing"
)

func TestCredentialReplacementIntentHash(t *testing.T) {
	input := CredentialReplacementInput{RequestID: "87b043bb-cb96-419c-b9a2-cab04814b65e", Name: "Replacement", Secret: "original-secret", Reason: "Reviewed replacement"}
	original := credentialReplacementHash("actor", "source", "validator", input)
	input.Secret = "changed-secret"
	if credentialReplacementHash("actor", "source", "validator", input) != original {
		t.Fatal("non-sensitive intent hash includes a secret")
	}
	for _, changed := range []string{
		credentialReplacementHash("different actor", "source", "validator", input),
		credentialReplacementHash("actor", "different source", "validator", input),
		credentialReplacementHash("actor", "source", "different validator", input),
		credentialReplacementHash("actor", "source", "validator", CredentialReplacementInput{Name: "Other", Reason: input.Reason}),
		credentialReplacementHash("actor", "source", "validator", CredentialReplacementInput{Name: input.Name, Reason: "Other"}),
	} {
		if changed == original {
			t.Fatal("different public intent preserved its hash")
		}
	}
}

func TestCredentialReplacementSecretComparison(t *testing.T) {
	for _, values := range [][2]string{{"secret", "different"}, {"secret", "Secret"}, {"short", strings.Repeat("s", 2048)}} {
		if sameCredentialReplacementSecret(values[0], values[1]) {
			t.Fatal("different secret accepted")
		}
	}
	if !sameCredentialReplacementSecret("测试 secret", "测试 secret") {
		t.Fatal("identical secret rejected")
	}
}

func TestCredentialReplacementRequestIDValidation(t *testing.T) {
	if !credentialReplacementRequestID.MatchString("87b043bb-cb96-419c-b9a2-cab04814b65e") {
		t.Fatal("canonical random UUID rejected")
	}
	for _, value := range []string{"", "87B043BB-CB96-419C-B9A2-CAB04814B65E", "00000000-0000-0000-0000-000000000000", "87b043bb-cb96-519c-b9a2-cab04814b65e", "87b043bb-cb96-419c-79a2-cab04814b65e", " 87b043bb-cb96-419c-b9a2-cab04814b65e"} {
		if credentialReplacementRequestID.MatchString(value) {
			t.Fatal("invalid request ID accepted")
		}
	}
}
