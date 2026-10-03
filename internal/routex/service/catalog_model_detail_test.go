package service

import (
	"context"
	"strings"
	"testing"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestAdminModelDetailRequiresBoundedResourceIdentity(t *testing.T) {
	for _, value := range []string{"", "mdl/one", "mdl_one ", " mdl_one", "mdl_one\n", "mdl_" + strings.Repeat("a", 27), "MDL_one", "usr_one", string([]byte{0xff})} {
		if validAdminModelTarget(value) {
			t.Fatal("unsafe or wrong resource target accepted", value)
		}
		if _, err := (&Service{}).GetAdminModel(context.Background(), "usr_actor", value); err != apperrors.ErrBadRequest {
			t.Fatal("invalid target reached authorized persistence lookup", value, err)
		}
	}
	for _, value := range []string{"mdl_legacy", "mdl_ExactCase", "mdl_" + strings.Repeat("a", 26)} {
		if !validAdminModelTarget(value) {
			t.Fatal("exact persisted legacy resource shape rejected", value)
		}
	}
}

func TestScopedProviderModelPriceRejectsUnsafeTargetsBeforeLookup(t *testing.T) {
	for _, value := range []string{"", "pmd/one", "pmd_one ", " pmd_one", "pmd_one\n", strings.Repeat("a", 31), string([]byte{0xff})} {
		if _, err := (&Service{}).GetPrice(context.Background(), "usr_actor", value); err != apperrors.ErrBadRequest {
			t.Fatal("scoped price lookup accepted unsafe target", value, err)
		}
	}
}
