package database

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExactTextContains searches a literal identifier fragment case-sensitively.
// GORM has no portable collation-independent LIKE expression; MySQL needs binary
// operands to avoid case/accent aliases. Fixed columns and escaped, bound values
// keep search separate from authority equality and SQL syntax.
func ExactTextContains(db *gorm.DB, column clause.Column, value string) clause.Expression {
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
	if db.Name() == "mysql" {
		return clause.Expr{SQL: "CAST(? AS BINARY) LIKE CAST(? AS BINARY) ESCAPE '!'", Vars: []any{column, pattern}}
	}
	return clause.Expr{SQL: "? LIKE ? ESCAPE '!'", Vars: []any{column, pattern}}
}
