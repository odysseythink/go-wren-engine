package converter

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// duckdbRule is the local SqlRewrite interface for DuckDBSqlConverter.
// Mirrors Java io.wren.main.sql.SqlRewrite.
type duckdbRule interface {
	Apply(root ast.Statement) ast.Statement
}

// DuckDBSqlConverter mirrors Java DuckDBSqlConverter: parse → RewriteArray →
// RewriteFunction → format(.., DUCKDB).
type DuckDBSqlConverter struct{}

func (c *DuckDBSqlConverter) Convert(sqlText string, _ *analyzer.SessionContext) (string, error) {
	stmt, err := parser.ParseSQL(sqlText)
	if err != nil {
		return "", fmt.Errorf("duckdb converter parse: %w", err)
	}
	rules := []duckdbRule{
		RewriteArray{},
		RewriteFunction{},
	}
	for _, r := range rules {
		stmt = r.Apply(stmt)
	}
	return formatter.FormatSQLDialect(stmt, formatter.DialectDuckDB), nil
}
