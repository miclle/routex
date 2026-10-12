package database

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	_ gorm.Dialector                         = postgresErrorDialector{}
	_ gorm.ErrorTranslator                   = postgresErrorDialector{}
	_ gorm.SavePointerDialectorInterface     = postgresErrorDialector{}
	_ interface{ Apply(*gorm.Config) error } = postgresErrorDialector{}
)

func TestPostgresRestrictErrorTranslation(t *testing.T) {
	dialector := postgresErrorDialector{Dialector: &postgres.Dialector{Config: &postgres.Config{}}}
	restrict := &pgconn.PgError{Code: "23001", Message: "restricted evidence deletion"}
	for _, input := range []error{restrict, fmt.Errorf("wrapped restriction: %w", restrict)} {
		translated := dialector.Translate(input)
		var retained *pgconn.PgError
		if !errors.Is(translated, gorm.ErrForeignKeyViolated) || !errors.Is(translated, input) ||
			!errors.As(translated, &retained) || retained != restrict {
			t.Fatal("RESTRICT must expose portable classification and retain the original error", translated)
		}
	}
	for _, test := range []struct {
		code string
		want error
	}{
		{"23503", gorm.ErrForeignKeyViolated},
		{"23505", gorm.ErrDuplicatedKey},
		{"23514", gorm.ErrCheckConstraintViolated},
		{"42703", gorm.ErrInvalidField},
	} {
		input := &pgconn.PgError{Code: test.code, Message: "upstream translation"}
		translated, upstream := dialector.Translate(input), dialector.Dialector.Translate(input)
		if !errors.Is(translated, test.want) || !errors.Is(translated, input) || translated.Error() != upstream.Error() {
			t.Fatal("existing PostgreSQL translations must be delegated unchanged", test.code, translated)
		}
	}
	unrelated := errors.New("unrelated error")
	for _, input := range []error{
		nil, unrelated, gorm.ErrDuplicatedKey,
		&pgconn.PgError{Code: "XX000", Message: "unmapped error"},
		fmt.Errorf("upstream wrapped error: %w", &pgconn.PgError{Code: "23503"}),
	} {
		if translated := dialector.Translate(input); translated != dialector.Dialector.Translate(input) {
			t.Fatal("unrelated, nil and unsupported wrapped translations must retain upstream behavior", translated)
		}
	}
}
