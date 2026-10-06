package limits

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMonthlyBehaviorCanonicalLegacyAndIndependentModes(t *testing.T) {
	base, err := Normalize(Policy{})
	if err != nil {
		t.Fatal(err)
	}
	hard, err := Normalize(Policy{TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"})
	if err != nil || !reflect.DeepEqual(base, hard) {
		t.Fatal("legacy stop/equality changed", err)
	}
	before, _ := json.Marshal(base)
	after, _ := json.Marshal(hard)
	if string(before) != string(after) {
		t.Fatal("legacy JSON changed")
	}
	for _, token := range []string{"", "stop", "alert_only"} {
		for _, money := range []string{"", "stop", "alert_only"} {
			p, err := Normalize(Policy{TokensMonthBehavior: token, MoneyMonthBehavior: money})
			if err != nil || (p.TokensMonthBehavior == "alert_only") != (token == "alert_only") || (p.MoneyMonthBehavior == "alert_only") != (money == "alert_only") {
				t.Fatal("independent mode lost", token, money, err)
			}
		}
	}
	for _, invalid := range []string{"STOP", "ALERT_ONLY", "alert_only ", " stop", "warn", "\x00stop"} {
		for _, p := range []Policy{{TokensMonthBehavior: invalid}, {MoneyMonthBehavior: invalid}} {
			if _, err := Normalize(p); err == nil {
				t.Fatal("invalid mode normalized", invalid)
			}
		}
	}
}

func TestMonthlyBehaviorParentCapacityAndCurrency(t *testing.T) {
	hundred, twoHundred := int64(100), int64(200)
	one, two := "1", "2"
	parent := Policy{TokensMonth: &hundred, MoneyMonth: &one, Currency: "USD"}
	child := Policy{TokensMonth: &twoHundred, MoneyMonth: &two, Currency: "USD"}
	if Narrower(parent, child) {
		t.Fatal("hard parent expanded")
	}
	parent.TokensMonthBehavior = MonthlyBehaviorAlertOnly
	if Narrower(parent, child) {
		t.Fatal("token mode weakened hard money")
	}
	parent.MoneyMonthBehavior = MonthlyBehaviorAlertOnly
	if !Narrower(parent, child) {
		t.Fatal("soft parent blocked independent hard child")
	}
	child.Currency = "EUR"
	if Narrower(parent, child) {
		t.Fatal("soft mode bypassed currency")
	}
	child.Currency = "USD"
	parent.RPM = &hundred
	child.RPM = &twoHundred
	if Narrower(parent, child) {
		t.Fatal("soft mode bypassed rate")
	}
	parent.RPM = nil
	child.RPM = nil
	parent.Tokens5H = &hundred
	child.Tokens5H = &twoHundred
	if Narrower(parent, child) {
		t.Fatal("soft mode bypassed rolling window")
	}
}
