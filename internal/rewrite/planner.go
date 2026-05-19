package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// AllRules is the ordered rule pipeline. Mirrors Java WrenPlanner.ALL_RULES.
var AllRules = []WrenRule{
	&GenerateViewRewrite{},
	&MetricRollupRewrite{},
	&WrenSqlRewrite{},
	&EnumRewrite{},
}

// Rewrite applies every rule in order, re-parsing the formatted SQL between
// rules so rules cannot interfere. Mirrors Java WrenPlanner.rewrite.
func Rewrite(sql string, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (string, error) {
	statement, err := parseSQL(sql)
	if err != nil {
		return "", err
	}
	for _, rule := range AllRules {
		reparsed, err := parseSQL(formatter.FormatSQL(statement))
		if err != nil {
			return "", fmt.Errorf("re-parse before %T: %w", rule, err)
		}
		statement, err = rule.Apply(reparsed, sessionContext, analyzedMDL)
		if err != nil {
			return "", err
		}
	}
	return formatter.FormatSQL(statement), nil
}
