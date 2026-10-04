package service

import (
	"encoding/csv"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

var callExportTestMemberColumns = []string{
	"request_id", "model_id", "model_name", "key_id", "protocol", "status", "stream",
	"started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens",
	"cache_read_tokens", "cache_write_tokens", "image_inputs", "pdf_inputs",
	"pricing_status", "charge_amount", "charge_currency",
}

func parseCallExport(t *testing.T, encoded []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(string(encoded))).ReadAll()
	if err != nil {
		t.Fatalf("invalid encoded CSV: %v", err)
	}
	return rows
}

func callExportTestColumn(t *testing.T, header []string, name string) int {
	t.Helper()
	index := slices.Index(header, name)
	if index < 0 {
		t.Fatalf("missing column %q", name)
	}
	return index
}

func TestSafeCSVCellProtectsSpreadsheetFormulaPrefixes(t *testing.T) {
	for _, value := range []string{
		"=SUM(1,2)", "+cmd", "-1+2", "@cmd", " \t=cmd", "\x01\r+cmd", "\u2003-1",
	} {
		if protected := safeCSVCell(value); protected != "'"+value {
			t.Fatalf("safeCSVCell(%q) = %q", value, protected)
		}
	}
	for _, value := range []string{"", "Text model", "text,model", "text\nmodel", "'literal", "  ordinary"} {
		if protected := safeCSVCell(value); protected != value {
			t.Fatalf("ordinary value %q changed to %q", value, protected)
		}
	}
	var output strings.Builder
	writer := csv.NewWriter(&output)
	protected := safeCSVCell("\t=HYPERLINK(\"https://invalid.example\")")
	if err := writer.Write([]string{protected, "comma,value", "quote\"value", "line\nvalue"}); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	rows := parseCallExport(t, []byte(output.String()))
	if writer.Error() != nil || len(rows) != 1 || rows[0][0] != protected || rows[0][1] != "comma,value" || rows[0][2] != "quote\"value" || rows[0][3] != "line\nvalue" {
		t.Fatalf("CSV quoting changed protected cells: %v, %v", writer.Error(), rows)
	}
}

func TestEncodeCallCSVPreservesSafeFieldsAndRedaction(t *testing.T) {
	zero, input, cacheRead, images := int64(0), int64(17), int64(4), int64(2)
	amount, currency := "0.001200", "USD"
	started := time.Date(2026, 9, 29, 10, 11, 12, 123456789, time.FixedZone("test", 8*60*60))
	record := entity.CallRecord{
		CallPricingFields: entity.CallPricingFields{CacheReadTokens: &cacheRead, PricingStatus: "=priced", ChargeAmount: &amount, ChargeCurrency: &currency},
		RequestID:         "=request",
		UserID:            "+user",
		ProjectID:         "-project",
		KeyID:             "@key",
		ModelID:           "\t=model",
		ModelName:         "\u2003+model",
		ProviderModelID:   "pmd_private",
		ConnectionID:      "con_private",
		RouteStopReason:   "unsafe_to_replay",
		Protocol:          "=protocol",
		Status:            "+status",
		Stream:            true,
		StartedAt:         started,
		CompletedAt:       started.Add(time.Second),
		DurationMS:        1000,
		InputTokens:       &input,
		OutputTokens:      &zero,
		ImageInputs:       &images,
		ErrorCode:         "private_error",
	}

	memberCSV, err := encodeCallCSV([]entity.CallRecord{record}, false)
	if err != nil {
		t.Fatal(err)
	}
	member := parseCallExport(t, memberCSV)
	if len(member) != 2 || !slices.Equal(member[0], callExportTestMemberColumns) {
		t.Fatalf("member CSV shape = %v", member)
	}
	for _, forbidden := range []string{"user_id", "project_id", "provider_model_id", "connection_id", "error_code", "route_stop_reason", "attempts", "price_etag", "pricing_snapshot"} {
		if slices.Contains(member[0], forbidden) || strings.Contains(string(memberCSV), "private") {
			t.Fatalf("member CSV exposed %s", forbidden)
		}
	}
	for _, column := range []string{"request_id", "model_id", "model_name", "key_id", "protocol", "status", "pricing_status"} {
		if value := member[1][callExportTestColumn(t, member[0], column)]; !strings.HasPrefix(value, "'") {
			t.Fatalf("formula-capable %s was not protected: %q", column, value)
		}
	}
	if got := member[1][callExportTestColumn(t, member[0], "input_tokens")]; got != "17" {
		t.Fatalf("input tokens = %q", got)
	}
	if got := member[1][callExportTestColumn(t, member[0], "output_tokens")]; got != "0" {
		t.Fatalf("explicit zero output tokens = %q", got)
	}
	for _, column := range []string{"cache_write_tokens", "pdf_inputs"} {
		if got := member[1][callExportTestColumn(t, member[0], column)]; got != "" {
			t.Fatalf("unknown %s became %q", column, got)
		}
	}
	if got := member[1][callExportTestColumn(t, member[0], "charge_amount")]; got != amount {
		t.Fatalf("exact decimal changed to %q", got)
	}
	if got := member[1][callExportTestColumn(t, member[0], "started_at")]; got != started.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("timestamp = %q", got)
	}
	if got := member[1][callExportTestColumn(t, member[0], "stream")]; got != strconv.FormatBool(record.Stream) {
		t.Fatalf("stream = %q", got)
	}

	platformCSV, err := encodeCallCSV([]entity.CallRecord{record}, true)
	if err != nil {
		t.Fatal(err)
	}
	platform := parseCallExport(t, platformCSV)
	platformColumns := append(append([]string{}, callExportTestMemberColumns...), "user_id", "project_id", "team_id", "team_membership_id")
	if len(platform) != 2 || !slices.Equal(platform[0], platformColumns) {
		t.Fatalf("platform CSV shape = %v", platform)
	}
	for _, column := range []string{"user_id", "project_id"} {
		if value := platform[1][callExportTestColumn(t, platform[0], column)]; !strings.HasPrefix(value, "'") {
			t.Fatalf("platform %s was not formula protected: %q", column, value)
		}
	}
}

func TestEncodeCallCSVIsCompleteOrBounded(t *testing.T) {
	empty, err := encodeCallCSV(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseCallExport(t, empty)
	if len(rows) != 1 || !slices.Equal(rows[0], callExportTestMemberColumns) {
		t.Fatalf("empty export = %v", rows)
	}

	overflow := make([]entity.CallRecord, callExportRows+1)
	atLimit, err := encodeCallCSV(overflow[:callExportRows], false)
	if err != nil {
		t.Fatalf("exact row limit failed: %v", err)
	}
	if rows := parseCallExport(t, atLimit); len(rows) != callExportRows+1 {
		t.Fatalf("exact row limit encoded %d CSV rows", len(rows))
	}
	if encoded, err := encodeCallCSV(overflow, false); err == nil || len(encoded) != 0 {
		t.Fatalf("row overflow returned %d bytes, error %v", len(encoded), err)
	}
	huge := entity.CallRecord{ModelName: strings.Repeat("x", callExportBytes+1)}
	if encoded, err := encodeCallCSV([]entity.CallRecord{huge}, false); err == nil || len(encoded) != 0 {
		t.Fatalf("byte overflow returned %d bytes, error %v", len(encoded), err)
	}
}

func TestCallExportPlatformUserFilterKeepsExactActingUserAcrossSubjects(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, userID := range []string{"usr_exact", "USR_EXACT", "usr_exact "} {
			t.Run(dialect.Name()+"/"+userID, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				query := callExportScope(db.Model(&entity.CallRecord{}), "usr_operator", "admin", "", CallFilter{UserID: userID})
				query.Statement.Clauses["WHERE"].Build(query.Statement)
				sql := query.Statement.SQL.String()
				expected := `"user_id" = ?`
				if dialect.Name() == "mysql" {
					expected = `CAST("user_id" AS BINARY) = CAST(? AS BINARY)`
				}
				if sql != "WHERE "+expected || !slices.Equal(query.Statement.Vars, []any{userID}) {
					t.Fatalf("platform user filter excludes Team facts or aliases recorded actor: SQL=%s bindings=%v", sql, query.Statement.Vars)
				}
			})
		}
	}
}

func TestCallExportPersonalAndProjectScopeRemainIsolated(t *testing.T) {
	for _, test := range []struct {
		scope, project, sql string
		vars                []any
	}{
		{"personal", "", "user_id = ? AND project_id = ? AND team_id = ?", []any{"usr_exact", "", ""}},
		{"project", "prj_exact", "project_id = ?", []any{"prj_exact"}},
		{"admin", "", "", nil},
	} {
		t.Run(test.scope, func(t *testing.T) {
			db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			query := callExportScope(db.Model(&entity.CallRecord{}), "usr_exact", test.scope, test.project, CallFilter{})
			if test.sql != "" {
				query.Statement.Clauses["WHERE"].Build(query.Statement)
			}
			if strings.TrimPrefix(query.Statement.SQL.String(), "WHERE ") != test.sql || !slices.Equal(query.Statement.Vars, test.vars) {
				t.Fatal("export scope widened", query.Statement.SQL.String(), query.Statement.Vars)
			}
		})
	}
}
