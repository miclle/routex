package limits

import "testing"

func quotaNumber(value int64) *int64  { return &value }
func quotaMoney(value string) *string { return &value }
func TestQuotaPolicyNormalizationAndNarrowing(t *testing.T) {
	policy, err := Normalize(Policy{Tokens5H: quotaNumber(0), Tokens7D: quotaNumber(100), MoneyMonth: quotaMoney("1.000000000000000001"), Currency: "USD"})
	if err != nil || *policy.Tokens5H != 0 || *policy.MoneyMonth != "1.000000000000000001" {
		t.Fatal(policy, err)
	}
	if _, err := Normalize(Policy{TPM: quotaNumber(-1)}); err == nil {
		t.Fatal("negative cap")
	}
	if _, err := Normalize(Policy{MoneyMonth: quotaMoney("1e2"), Currency: "USD"}); err == nil {
		t.Fatal("floating point notation accepted")
	}
	if _, err := Normalize(Policy{MoneyMonth: quotaMoney("1"), Currency: ""}); err == nil {
		t.Fatal("implicit money unit")
	}
	child := Policy{Tokens5H: quotaNumber(1)}
	if Narrower(policy, child) {
		t.Fatal("zero broadened")
	}
	child = Policy{MoneyMonth: quotaMoney("1.000000000000000002"), Currency: "USD"}
	if Narrower(policy, child) {
		t.Fatal("decimal precision lost")
	}
	child.MoneyMonth = quotaMoney("1")
	if !Narrower(policy, child) {
		t.Fatal("narrower policy rejected")
	}
	child.Currency = "EUR"
	if Narrower(policy, child) {
		t.Fatal("cross currency narrowing guessed")
	}
	if !Narrower(policy, Policy{}) {
		t.Fatal("inheritance rejected")
	}
	if value := MoneyMinimum(quotaMoney("1.000000000000000001"), quotaMoney("1.000000000000000002")); value == nil || *value != "1.000000000000000001" {
		t.Fatal("inexact effective minimum")
	}
}
