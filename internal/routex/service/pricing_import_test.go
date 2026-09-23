package service

import (
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
)

func priceCSV(rows ...string) string {
	return strings.Join(priceCSVColumns, ",") + "\n" + strings.Join(rows, "\n") + "\n"
}
func TestPriceCSVNormalizesDecimalsAndBindsSemanticContent(t *testing.T) {
	a := parsePriceCSV(priceCSV("pmo_b,OUTPUT_TOKEN,base,1M_TOKEN,USD,0.000000000000000001,false,0", "pmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,1.00,true,"))
	b := parsePriceCSV("\ufeffupstream_name,amount,currency,enabled,tier,metric,provider_model_id,context_threshold,unit\r\nUntrusted,1,USD,true,base,INPUT_TOKEN,pmo_a,,1M_TOKEN\r\nIgnored,0.000000000000000001,USD,false,base,OUTPUT_TOKEN,pmo_b,0,1M_TOKEN\r\n")
	if len(a.Errors) != 0 || len(b.Errors) != 0 {
		t.Fatalf("valid CSV rejected: %v %v", a.Errors, b.Errors)
	}
	if a.Items[0].Rates[0].Amount != "1" || a.Items[1].Rates[0].Enabled || a.Items[1].Rates[0].Amount != "0.000000000000000001" {
		t.Fatal("decimal precision or explicit disabled state lost")
	}
	if priceImportDigest("etag", a.Items) != priceImportDigest("etag", b.Items) {
		t.Fatal("format/order/display-only names changed semantic digest")
	}
	if priceImportDigest("other", a.Items) == priceImportDigest("etag", a.Items) {
		t.Fatal("digest did not bind catalogue ETag")
	}
	b.Items[0].Rates[0].Amount = "2"
	if priceImportDigest("etag", a.Items) == priceImportDigest("etag", b.Items) {
		t.Fatal("edited amount kept preview digest")
	}
}
func TestPriceCSVReportsAllIndependentRowErrors(t *testing.T) {
	parsed := parsePriceCSV(priceCSV("pmo_a,BAD,unknown,request,ZZZ,1e3,yes,7", "pmo_b,INPUT_TOKEN,base,1M_TOKEN,USD,-1,,0"))
	if len(parsed.Errors) != 9 {
		t.Fatalf("reported %d errors, want9: %+v", len(parsed.Errors), parsed.Errors)
	}
	for _, issue := range parsed.Errors {
		if issue.Row < 2 || issue.Row > 3 || issue.Code == "" || issue.Column == "" {
			t.Fatal("row error cannot be located")
		}
	}
}
func TestPriceCSVDuplicatesDimensionsAndBounds(t *testing.T) {
	for _, test := range []struct{ name, raw, code string }{
		{"duplicate", priceCSV("pmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,", "pmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,1,false,"), "duplicate_rate"},
		{"threshold", priceCSV("pmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,", "pmo_a,OUTPUT_TOKEN,base,1M_TOKEN,USD,1,false,0"), "inconsistent_threshold"},
		{"unknown dimension", strings.Join(priceCSVColumns, ",") + ",batch\npmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,,true\n", "unsupported_column"},
		{"source spoof", strings.Join(priceCSVColumns, ",") + ",source\npmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,,repository\n", "unsupported_column"},
		{"size", strings.Repeat("x", priceImportBytes+1), "file_too_large"},
		{"encoding", string([]byte{255}), "invalid_encoding"},
		{"empty", priceCSV(), "empty_file"},
		{"columns", priceCSV("pmo_a,INPUT_TOKEN"), "column_count"},
		{"quote", priceCSV(`pmo_a,"INPUT_TOKEN`), "invalid_csv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := parsePriceCSV(test.raw)
			found := false
			for _, issue := range parsed.Errors {
				found = found || issue.Code == test.code
			}
			if !found {
				t.Fatalf("missing %s: %+v", test.code, parsed.Errors)
			}
		})
	}
	rows := []string{}
	for index := range priceImportModels + 1 {
		rows = append(rows, fmt.Sprintf("pmo_%d,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,", index))
	}
	parsed := parsePriceCSV(priceCSV(rows...))
	if parsed.Errors[len(parsed.Errors)-1].Code != "too_many_models" {
		t.Fatal("model batch bound not enforced")
	}
	rows = make([]string, priceImportRows+1)
	for index := range rows {
		rows[index] = "pmo_a,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,"
	}
	parsed = parsePriceCSV(priceCSV(rows...))
	if parsed.Errors[len(parsed.Errors)-1].Code != "too_many_rows" {
		t.Fatal("row bound not enforced")
	}
}
func TestPriceCSVNameFormulaProtection(t *testing.T) {
	for _, name := range []string{"=SUM(1,2)", "+cmd", "-cmd", "@cmd", " \t=cmd", "\x01\r+cmd", "\u2003-1"} {
		protected := safePriceCSVName(name)
		if protected != "'"+name {
			t.Fatalf("unsafe name: %q", protected)
		}
		var out strings.Builder
		writer := csv.NewWriter(&out)
		if err := writer.Write([]string{protected}); err != nil {
			t.Fatal(err)
		}
		writer.Flush()
		row, err := csv.NewReader(strings.NewReader(out.String())).Read()
		if err != nil || row[0] != protected {
			t.Fatal("CSV quoting lost formula guard")
		}
	}
	for _, name := range []string{"Text model", "text,model", "text\nmodel"} {
		if safePriceCSVName(name) != name {
			t.Fatal("ordinary name changed")
		}
	}
}
