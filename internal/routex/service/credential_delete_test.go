package service

import (
	"strings"
	"testing"
)

func TestCredentialDeleteTargetValidation(t *testing.T) {
	credentialID := "crd_01m36yee4gkbns18pfcqqc75a3"
	etag := strings.Repeat("a", 64)
	if !validCredentialDeleteTarget(credentialID, etag) {
		t.Fatal("valid deletion target rejected")
	}
	for _, target := range [][2]string{
		{"crd_missing", etag}, {"usr_01m36yee4gkbns18pfcqqc75a3", etag},
		{"crd_zzzzzzzzzzzzzzzzzzzzzzzzzz", etag}, {"crd_81m36yee4gkbns18pfcqqc75a3", etag},
		{strings.ToUpper(credentialID), etag}, {credentialID, ""}, {credentialID, "0"},
		{credentialID, strings.Repeat("g", 64)}, {credentialID, strings.Repeat("a", 65)},
	} {
		if validCredentialDeleteTarget(target[0], target[1]) {
			t.Fatal("invalid deletion target accepted")
		}
	}
}
