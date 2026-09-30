package service

import (
	"context"
	"testing"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestQuotaSettingsRejectHostDependentCalendar(t *testing.T) {
	for _, zone := range []string{"Local", " Local "} {
		t.Run(zone, func(t *testing.T) {
			_, err := (&Service{}).WriteQuotaSettings(context.Background(), "actor", "reviewed", QuotaSettingsInput{TimeZone: zone, Reason: "Stable installation calendar"})
			if err != apperrors.ErrBadRequest {
				t.Fatalf("host-dependent calendar must fail validation before persistence: %v", err)
			}
		})
	}
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			_, err := (&Service{}).WriteQuotaSettings(context.Background(), "actor", "reviewed", QuotaSettingsInput{TimeZone: zone, Reason: "Stable installation calendar"})
			if err != runtimeUnavailable {
				t.Fatalf("supported calendar must pass validation and reach runtime guard: %v", err)
			}
		})
	}
}
