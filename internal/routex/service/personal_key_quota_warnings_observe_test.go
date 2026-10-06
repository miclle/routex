package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func personalKeyWarningFixture() (entity.ResourceLimit, entity.APIKey, entity.User, *eventqueue.QuotaUsageProofBatch) {
	row, birth, usage := projectWarningFixture()
	row.ScopeKind = "key"
	row.ScopeID = "key_00000000000000000000000001"
	root := entity.APIKey{ID: row.ScopeID, UserID: "usr_00000000000000000000000001", Name: "Original root", Status: entity.KeyActive, LifecycleRevision: "kvr_00000000000000000000000001", CreatedAt: birth}
	owner := entity.User{ID: root.UserID, CreatedAt: birth.Add(-time.Hour)}
	frame := &eventqueue.QuotaUsageProofBatch{Active: true, AsOf: usage.AsOf, CoverageStart: usage.CoverageStart, TimeZone: usage.TimeZone, Accounts: map[string]eventqueue.QuotaUsageAccountProof{limitAccount("key", root.ID): {Registered: true, CreatedAt: birth, Usage: *usage}}}
	return row, root, owner, frame
}
func TestPersonalKeyWarningExactIndependentLevelsAndAtomicBirth(t *testing.T) {
	for _, name := range []string{"valid", "unregistered", "legacy_birth", "changed_birth", "missing", "alias", "inactive", "coverage", "sample", "timezone", "root_owner", "owner_birth", "owner_born_after_root", "tokens_unknown", "tokens_held", "tokens_active_held", "money_unknown", "money_held", "money_active_unknown", "mixed_currency", "current_currency", "zero", "unset", "exhausted", "holds_only", "name", "wrong_scope"} {
		t.Run(name, func(t *testing.T) {
			row, root, owner, frame := personalKeyWarningFixture()
			account := limitAccount("key", root.ID)
			proof := frame.Accounts[account]
			currency := "USD"
			want := 2
			switch name {
			case "unregistered":
				proof.Registered = false
				want = 0
			case "legacy_birth":
				proof.CreatedAt = time.Time{}
				want = 0
			case "changed_birth":
				proof.CreatedAt = proof.CreatedAt.Add(time.Millisecond)
				want = 0
			case "missing":
				delete(frame.Accounts, account)
				want = 0
			case "alias":
				frame.Accounts[strings.ToUpper(account)] = proof
				delete(frame.Accounts, account)
				want = 0
			case "inactive":
				frame.Active = false
				want = 0
			case "coverage":
				frame.CoverageStart = frame.AsOf.Add(-time.Hour)
				proof.Usage.CoverageStart = frame.CoverageStart
				want = 0
			case "sample":
				proof.Usage.AsOf = frame.AsOf.Add(time.Second)
				want = 0
			case "timezone":
				proof.Usage.TimeZone = "Etc/UTC"
				want = 0
			case "root_owner":
				root.UserID = "usr_other"
				want = 0
			case "owner_birth":
				owner.CreatedAt = time.Time{}
				want = 0
			case "owner_born_after_root":
				owner.CreatedAt = root.CreatedAt.Add(time.Millisecond)
				want = 0
			case "tokens_unknown":
				proof.Usage.Month.TokensUnknown = 1
				want = 1
			case "tokens_held":
				proof.Usage.Month.TokensHeld = 1
				want = 1
			case "tokens_active_held":
				proof.Usage.Active.TokensHeld = 1
				want = 1
			case "money_unknown":
				proof.Usage.Month.MoneyUnknown = 1
				want = 1
			case "money_held":
				proof.Usage.Month.MoneyHeld = map[string]string{"USD": "0.000000000000000001"}
				want = 1
			case "money_active_unknown":
				proof.Usage.Active.MoneyUnknown = 1
				want = 1
			case "mixed_currency":
				proof.Usage.Month.MoneyUsed["EUR"] = "0"
				want = 1
			case "current_currency":
				currency = "EUR"
				want = 1
			case "zero":
				z := int64(0)
				m := "0"
				row.TokensMonth = &z
				row.MoneyMonth = &m
				want = 0
			case "unset":
				row.TokensMonth = nil
				row.MoneyMonth = nil
				want = 0
			case "exhausted":
				proof.Usage.Month.TokensUsed = 100
				proof.Usage.Month.MoneyUsed["USD"] = "0.000000000000000010"
				want = 0
			case "holds_only":
				proof.Usage.Month.TokensUsed = 0
				proof.Usage.Month.MoneyUsed = nil
				proof.Usage.Month.TokensHeld = 90
				want = 0
			case "name":
				root.Name = ""
				want = 0
			case "wrong_scope":
				row.ScopeKind = "project"
				want = 0
			}
			if name != "missing" && name != "alias" {
				frame.Accounts[account] = proof
			}
			before := frame.Accounts[account]
			got := personalKeyMonthlyWarnings(row, root, owner, frame, currency)
			if len(got) != want {
				t.Fatalf("%s count=%d want=%d", name, len(got), want)
			}
			if !reflect.DeepEqual(before, frame.Accounts[account]) {
				t.Fatal("read changed journal proof")
			}
			if name == "valid" && (got[0].Level != "near" || got[1].Level != "critical" || got[1].Settled != "0.000000000000000009" || got[1].Limit != "0.00000000000000001") {
				t.Fatal("exact settled amounts lost")
			}
		})
	}
}
func TestPersonalKeyWarningBoundaryAndImmutableDedup(t *testing.T) {
	for _, tt := range []struct{ used, level string }{{"0.000000000000000007", ""}, {"0.000000000000000008", "near"}, {"0.000000000000000009", "critical"}, {"0.000000000000000010", ""}} {
		r, k, u, f := personalKeyWarningFixture()
		r.TokensMonth = nil
		p := f.Accounts[limitAccount("key", k.ID)]
		p.Usage.Month.MoneyUsed["USD"] = tt.used
		f.Accounts[limitAccount("key", k.ID)] = p
		got := personalKeyMonthlyWarnings(r, k, u, f, "USD")
		if tt.level == "" && len(got) != 0 || tt.level != "" && (len(got) != 1 || got[0].Level != tt.level) {
			t.Fatal("rational boundary", tt, got)
		}
	}
	r, k, u, f := personalKeyWarningFixture()
	old := personalKeyMonthlyWarnings(r, k, u, f, "USD")[0]
	fresh := old
	fresh.Settled = "85"
	fresh.AsOf = fresh.AsOf.Add(time.Minute)
	fresh.RootKeyName = "Later rename"
	if !samePersonalKeyWarningIdentity(old, fresh) {
		t.Fatal("replay overwrites immutable first snapshot")
	}
	for _, name := range []string{"root", "owner", "owner_birth", "owner_born_after_root", "root_birth", "month", "revision", "level", "currency", "dimension", "generation"} {
		v := old
		switch name {
		case "root":
			v.RootKeyID = strings.ToUpper(v.RootKeyID)
		case "owner":
			v.OwnerID += "x"
		case "owner_birth", "owner_born_after_root":
			v.OwnerCreatedAt = v.OwnerCreatedAt.Add(time.Millisecond)
		case "root_birth":
			v.ResourceCreatedAt = v.ResourceCreatedAt.Add(time.Millisecond)
		case "month":
			v.MonthStart = v.MonthEnd
		case "revision":
			v.PolicyRevision += "x"
		case "level":
			v.Level = "critical"
			v.Threshold = 90
		case "currency":
			v.Currency = "USD"
		case "dimension":
			v.Dimension = "money"
		case "generation":
			v.ThresholdGeneration += "x"
		}
		if samePersonalKeyWarningIdentity(old, v) {
			t.Fatal("distinct dedup identity", name)
		}
	}
}
