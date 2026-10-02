package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExactText compares an authority identifier byte-for-byte. GORM has no portable
// collation-independent text equality operator: MySQL's default collation can
// alias case, accents and trailing spaces. Keep that adapter here, bind values
// through GORM, and supply only fixed, server-owned column names.
func ExactText(db *gorm.DB, column clause.Column, value string) clause.Expression {
	if db.Name() == "mysql" {
		return clause.Expr{SQL: "CAST(? AS BINARY) = CAST(? AS BINARY)", Vars: []any{column, value}}
	}
	return clause.Eq{Column: column, Value: value}
}

// ExactTextColumns applies the same authority comparison to a join. Both columns
// must be fixed, server-owned identifiers; GORM quotes them independently.
func ExactTextColumns(db *gorm.DB, left, right clause.Column) clause.Expression {
	if db.Name() == "mysql" {
		return clause.Expr{SQL: "CAST(? AS BINARY) = CAST(? AS BINARY)", Vars: []any{left, right}}
	}
	return clause.Eq{Column: left, Value: right}
}
