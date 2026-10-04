package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRepositoryPriceFrozenSchemasKeepExactColumnsAndPrivateHistory(t *testing.T) {
	for _, pair := range [][2]any{{&repositoryPriceSettingV49{}, &entity.RepositoryPriceSetting{}}, {&repositoryPriceMappingV49{}, &entity.RepositoryPriceMapping{}}, {&repositoryPriceReceiptV49{}, &entity.RepositoryPriceReceipt{}}} {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if frozen.Table != actual.Table || len(frozen.Relationships.Relations) != 0 || len(actual.Relationships.Relations) != 0 || len(frozen.Fields) != len(actual.Fields) {
			t.Fatal("schema acquired mutable relations")
		}
		for _, f := range frozen.Fields {
			a := actual.LookUpField(f.Name)
			if a == nil || f.Tag != a.Tag || f.DBName != a.DBName || f.FieldType != a.FieldType {
				t.Fatal("frozen divergence", frozen.Table, f.Name)
			}
		}
		if len(frozen.ParseCheckConstraints()) == 0 {
			t.Fatal("missing durable guards")
		}
	}
	receipt, err := schema.Parse(&repositoryPriceReceiptV49{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LookUpField("ReviewETag").DBName != "review_etag" || receipt.LookUpField("ConfigETag").DBName != "config_etag" || receipt.LookUpField("CatalogueETag").DBName != "catalogue_etag" {
		t.Fatal("review or proof column changed")
	}
	if !strings.Contains(receipt.ParseCheckConstraints()["ck_repository_price_receipt_mode"].Constraint, "ASCII(SUBSTRING(mode,") {
		t.Fatal("mode aliases accepted under case-insensitive collation")
	}
	if len(receipt.PrimaryFields) != 1 || receipt.PrimaryFields[0].DBName != "request_id" || receipt.PrimaryFields[0].Size != 36 {
		t.Fatal("intent lacks global UUID slot")
	}
	config, err := schema.Parse(&repositoryPriceSettingV49{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if config.LookUpField("ETag").DBName != "etag" {
		t.Fatal("check references nonexistent etag")
	}
	frozen, err := schema.Parse(&repositoryRateV49{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.PriceRate{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"RepositoryModelKey", "RepositoryRateKey"} {
		f, a := frozen.LookUpField(name), current.LookUpField(name)
		if f.Tag != a.Tag || f.FieldType.Kind() != reflect.Pointer {
			t.Fatal("historical custom default lost")
		}
	}
	if !strings.Contains(frozen.ParseCheckConstraints()["ck_price_rate_repository"].Constraint, "repository_rate_key IS NULL") {
		t.Fatal("partial provenance allowed")
	}
	if reflect.ValueOf(repositoryPriceMigration).IsNil() {
		t.Fatal("missing registry-callable V49")
	}
}
