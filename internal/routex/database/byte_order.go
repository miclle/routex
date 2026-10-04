package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ByteOrder provides exact byte keysets. GORM has no portable collation syntax;
// fixed server-owned columns are quoted and cursor values remain parameters.
func ByteOrder(db *gorm.DB, column clause.Column) clause.Expression {
	if db.Name() == "mysql" {
		return clause.Expr{SQL: "CAST(? AS BINARY)", Vars: []any{column}}
	}
	return clause.Expr{SQL: "? COLLATE \"C\"", Vars: []any{column}}
}
func ByteAfter(db *gorm.DB, column clause.Column, cursor string) clause.Expression {
	if db.Name() == "mysql" {
		return clause.Expr{SQL: "CAST(? AS BINARY) > CAST(? AS BINARY)", Vars: []any{column, cursor}}
	}
	return clause.Expr{SQL: "? COLLATE \"C\" > ? COLLATE \"C\"", Vars: []any{column, cursor}}
}
