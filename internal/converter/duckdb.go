package converter

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// DuckDBSqlConverter applies DuckDB-specific SQL rewrites.
type DuckDBSqlConverter struct{}

func (c *DuckDBSqlConverter) Convert(sql string, ctx *analyzer.SessionContext) string {
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return sql
	}
	return formatter.FormatSQLDialect(stmt, formatter.DialectDuckDB)
}
