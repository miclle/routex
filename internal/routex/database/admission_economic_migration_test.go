package database

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestAdmissionEconomicV119FrozenSchemas(t *testing.T) {
	pairs := []struct {
		frozen, current any
		table           string
	}{
		{&admissionEconomicDomainV119{}, &entity.AdmissionEconomicDomain{}, "admission_economic_domains"},
		{&admissionEconomicReceiptV119{}, &entity.AdmissionEconomicReceipt{}, "admission_economic_receipts"},
		{&admissionEconomicAttemptV119{}, &entity.AdmissionEconomicAttempt{}, "admission_economic_attempts"},
		{&admissionEconomicAccountV119{}, &entity.AdmissionEconomicAccount{}, "admission_economic_accounts"},
		{&admissionEconomicCounterV119{}, &entity.AdmissionEconomicCounter{}, "admission_economic_counters"},
		{&admissionEconomicContributionV119{}, &entity.AdmissionEconomicContribution{}, "admission_economic_contributions"},
		{&admissionEconomicInvalidBoundV119{}, &entity.AdmissionEconomicInvalidBound{}, "admission_economic_invalid_bounds"},
	}
	if len(admissionEconomicModelsV119()) != len(pairs) {
		t.Fatal("migration must own exactly seven tables")
	}
	for _, pair := range pairs {
		t.Run(pair.table, func(t *testing.T) {
			frozen, current := oidcV94Schema(t, pair.frozen), oidcV94Schema(t, pair.current)
			if frozen.Table != pair.table || current.Table != pair.table || !reflect.DeepEqual(frozen.DBNames, current.DBNames) {
				t.Fatal("frozen/current physical columns", frozen.DBNames, current.DBNames)
			}
			for _, name := range frozen.DBNames {
				a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
				if b == nil || a.FieldType != b.FieldType || a.Tag != b.Tag || a.HasDefaultValue || a.AutoIncrement {
					t.Fatal("frozen scalar/default drift", name)
				}
				if a.Tag.Get("json") != "-" {
					t.Fatal("economic state must not become a public DTO", name)
				}
				if a.DataType == schema.Bytes && (a.IndirectFieldType != reflect.TypeFor[[]byte]() || a.Size != 0) {
					t.Fatal("binary bodies must use portable native mapping", name)
				}
			}
			if len(frozen.Relationships.Relations) != len(current.Relationships.Relations) {
				t.Fatal("relationship count drift")
			}
			for name, relation := range frozen.Relationships.Relations {
				c := relation.ParseConstraint()
				live := current.Relationships.Relations[name]
				if live == nil || relation.Type != schema.BelongsTo || c == nil || c.Schema != frozen || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" || !strings.HasPrefix(c.ReferenceSchema.Table, "admission_economic_") {
					t.Fatal("history must have only restrictive economic-owned references", name)
				}
				if c.ReferenceSchema.Table != live.FieldSchema.Table || len(c.ForeignKeys) != len(c.References) {
					t.Fatal("frozen reference mapping", name)
				}
			}
			admissionEconomicCheckColumnsV119(t, frozen)
			admissionEconomicIndexBoundsV119(t, frozen)
		})
	}
}

func TestAdmissionEconomicV119HistoryAndProjectionGuards(t *testing.T) {
	receipt := oidcV94Schema(t, &admissionEconomicReceiptV119{})
	attempt := oidcV94Schema(t, &admissionEconomicAttemptV119{})
	for _, tail := range []struct {
		s                   *schema.Schema
		digest, body, check string
	}{
		{receipt, "terminal_digest", "terminal_body", "ck_economic_receipt_terminal"},
		{attempt, "checkpoint_digest", "checkpoint_body", "ck_economic_attempt_checkpoint"},
	} {
		if tail.s.FieldsByDBName[tail.digest].NotNull || tail.s.FieldsByDBName[tail.body].NotNull {
			t.Fatal("absent immutable tail must remain SQL NULL")
		}
		guard := tail.s.ParseCheckConstraints()[tail.check].Constraint
		for _, part := range []string{tail.digest + " IS NULL", tail.body + " IS NULL", tail.digest + " IS NOT NULL", tail.body + " IS NOT NULL", "BETWEEN 1 AND 131072"} {
			if !strings.Contains(guard, part) {
				t.Fatal("partial/empty/oversized tail admitted", tail.check, part)
			}
		}
	}
	if attempt.ParseCheckConstraints()["ck_economic_attempt_number"].Constraint != "number BETWEEN 1 AND 4" {
		t.Fatal("frozen dispatch budget changed")
	}
	domain := oidcV94Schema(t, &admissionEconomicDomainV119{})
	for _, part := range []string{"coverage_start = 0", "clock_high_water = 0", "receipt_count = 0", "coverage_start > 0", "clock_high_water >= coverage_start"} {
		if !strings.Contains(domain.ParseCheckConstraints()["ck_economic_domain_coverage"].Constraint, part) {
			t.Fatal("implicit coverage/clock initialization", part)
		}
	}
	if !strings.Contains(domain.ParseCheckConstraints()["ck_economic_domain_capacity"].Constraint, "receipt_count <= receipt_capacity") {
		t.Fatal("unbounded retained receipt count")
	}
	account := oidcV94Schema(t, &admissionEconomicAccountV119{})
	if account.FieldsByDBName["birth"].TagSettings["PRECISION"] != "6" || !strings.Contains(account.ParseCheckConstraints()["ck_economic_account_counts"].Constraint, "executor_count >= 0") {
		t.Fatal("birth or independent count guards missing")
	}
	contribution := oidcV94Schema(t, &admissionEconomicContributionV119{})
	guard := contribution.ParseCheckConstraints()["ck_economic_contribution_kind"].Constraint
	if !strings.Contains(guard, "ASCII(SUBSTRING(") || strings.Contains(guard, " IN (") {
		t.Fatal("collation aliases must not change contribution kind")
	}
	expiry := contribution.ParseCheckConstraints()["ck_economic_contribution_expiry"].Constraint
	for _, part := range []string{"expires_at > 0", "'rpm','terminal_quota'", "'executor','active_quota'", "expires_at IS NULL"} {
		if !strings.Contains(expiry, part) {
			t.Fatal("executor/economic expiry conflation", part)
		}
	}
	if receipt.FieldsByDBName["request_digest"].PrimaryKey != true || receipt.FieldsByDBName["request_id"].PrimaryKey || attempt.FieldsByDBName["attempt_id"].Unique {
		t.Fatal("case-sensitive identities require exact-byte lookup hashes")
	}
	if receipt.Relationships.Relations["Domain"].FieldSchema.Table != domain.Table {
		t.Fatal("history bound to mutable authority")
	}
}

func admissionEconomicCheckColumnsV119(t *testing.T, s *schema.Schema) {
	t.Helper()
	words := regexp.MustCompile(`[a-z][a-z0-9_]*`)
	stringsSQL := regexp.MustCompile(`'[^']*'`)
	for name, guard := range s.ParseCheckConstraints() {
		for _, word := range words.FindAllString(stringsSQL.ReplaceAllString(guard.Constraint, ""), -1) {
			if word == "true" || word == "false" {
				continue
			}
			if s.FieldsByDBName[word] == nil {
				t.Fatal("CHECK references absent physical column", name, word)
			}
		}
	}
}

func admissionEconomicIndexBoundsV119(t *testing.T, s *schema.Schema) {
	t.Helper()
	width := func(fields []*schema.Field) int {
		total := 0
		for _, field := range fields {
			switch field.DataType {
			case schema.String:
				total += 4 * field.Size // worst-case utf8mb4 bytes
			case schema.Int:
				total += 8
			default:
				t.Fatal("unbounded/binary indexed field", field.Name)
			}
		}
		return total
	}
	if size := width(s.PrimaryFields); size > 3072 {
		t.Fatal("MySQL primary index exceeds portable budget", size)
	}
	for _, index := range s.ParseIndexes() {
		fields := make([]*schema.Field, 0, len(index.Fields))
		for _, option := range index.Fields {
			if option.Length != 0 || option.Expression != "" {
				t.Fatal("prefix/expression index cannot establish exact identity", index.Name)
			}
			fields = append(fields, option.Field)
		}
		if size := width(fields); size > 3072 {
			t.Fatal("MySQL secondary index exceeds portable budget", index.Name, size)
		}
	}
}
