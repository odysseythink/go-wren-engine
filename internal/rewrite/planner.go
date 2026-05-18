package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// AllRules is the list of all rewrite rules applied sequentially.
var AllRules = []WrenRule{}

// Rewrite applies all rewrite rules to the SQL.
func Rewrite(sql string, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (string, error) {
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return "", err
	}
	for _, rule := range AllRules {
		sql = formatter.FormatSQL(stmt)
		stmt, err = parser.ParseSQL(sql)
		if err != nil {
			return "", err
		}
		stmt = rule.Apply(stmt, ctx, analyzedMDL)
	}
	return formatter.FormatSQL(stmt), nil
}
