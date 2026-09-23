package handler

import (
	"context"
	"reflect"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
)

func testPricingCurrencyRequirements(t *testing.T, db *gorm.DB, actorID string) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.GetPricingCurrency(ctx, actorID)
	if err != nil {
		t.Fatal(err)
	}
	rate := pricing.Rate{Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "EUR", Amount: "1", Enabled: false}
	page, err := svc.WritePrices(ctx, actorID, before.ETag, []service.PriceInput{{ProviderModelID: "pmo_price_b", Rates: []pricing.Rate{rate}}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := svc.GetPricingCurrency(ctx, actorID)
	if err != nil || current.ETag != page.ETag || !reflect.DeepEqual(current.RequiredCurrencies, []string{"USD"}) {
		t.Fatalf("disabled price changed required conversions: %+v %v", current, err)
	}
	fx := current.Currency
	fx.Rates["EUR"] = "8.2"
	page, err = svc.WritePricingCurrency(ctx, actorID, current.ETag, fx)
	if err != nil {
		t.Fatal(err)
	}
	rate.Enabled = true
	page, err = svc.WritePrices(ctx, actorID, page.ETag, []service.PriceInput{{ProviderModelID: "pmo_price_b", Rates: []pricing.Rate{rate}}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = svc.GetPricingCurrency(ctx, actorID)
	if err != nil || current.ETag != page.ETag || !reflect.DeepEqual(current.RequiredCurrencies, []string{"EUR", "USD"}) || current.Currency.Rates["EUR"] != "8.2" {
		t.Fatalf("currency requirements did not cover the complete catalogue: %+v %v", current, err)
	}
}
