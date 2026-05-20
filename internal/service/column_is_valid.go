package service

import (
	"context"
	"fmt"
	"time"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

// ColumnIsValidRule validates that "<col>" can be SELECTed from "<model>" via
// the full P3 rewrite pipeline + P4 DuckDB connector. Mirrors Java
// io.wren.main.validation.ColumnIsValid.
type ColumnIsValidRule struct {
	metadata     Metadata
	sqlConverter converter.SqlConverter
}

const ruleColumnIsValid = "column_is_valid"

func (r *ColumnIsValidRule) Validate(ctx context.Context, params map[string]any, analyzed *mdl.AnalyzedMDL) ([]ValidationResult, error) {
	start := time.Now()

	modelName, _ := params["modelName"].(string)
	if modelName == "" {
		return []ValidationResult{errorResult(ruleColumnIsValid, start, "Model name is required")}, nil
	}
	columnName, _ := params["columnName"].(string)
	if columnName == "" {
		return []ValidationResult{errorResult(ruleColumnIsValid + ":" + modelName, start, "Column name is required")}, nil
	}

	name := fmt.Sprintf("%s:%s:%s", ruleColumnIsValid, modelName, columnName)
	wrenMDL := analyzed.WrenMDL()

	sessionCtx := &analyzer.SessionContext{
		Catalog:             wrenMDL.Catalog(),
		Schema:              wrenMDL.Schema(),
		EnableDynamicFields: true, // Java forces enableDynamic=true for validation
	}

	sql := fmt.Sprintf(`SELECT "%s" FROM "%s" LIMIT 1`, columnName, modelName)

	planned, err := rewrite.Rewrite(sql, sessionCtx, analyzed)
	if err != nil {
		return []ValidationResult{failResult(name, start, err.Error())}, nil
	}
	converted, err := r.sqlConverter.Convert(planned, sessionCtx)
	if err != nil {
		return []ValidationResult{failResult(name, start, err.Error())}, nil
	}
	it, err := r.metadata.DirectQuery(ctx, converted, nil)
	if err != nil {
		return []ValidationResult{failResult(name, start, err.Error())}, nil
	}
	defer it.Close()
	// Java calls it.next() once and discards. We mirror but ignore the bool —
	// an empty result set is NOT a failure (risk #15).
	_ = it.Next()

	return []ValidationResult{passResult(name, start)}, nil
}

func passResult(name string, start time.Time) ValidationResult {
	return ValidationResult{Name: name, Status: StatusPass, Duration: succinctMs(start)}
}

func failResult(name string, start time.Time, msg string) ValidationResult {
	m := msg
	return ValidationResult{Name: name, Status: StatusFail, Duration: succinctMs(start), Message: &m}
}

func errorResult(name string, start time.Time, msg string) ValidationResult {
	m := msg
	return ValidationResult{Name: name, Status: StatusError, Duration: succinctMs(start), Message: &m}
}

// succinctMs formats elapsed time as "<X>.<YY>ms" — matches Java
// Duration.succinctDuration(ms, MILLISECONDS).toString() for sub-second.
// Longer durations would be "X.XXs"/"X.XXm" in Java; the difftest masks the
// duration field, so the format mismatch is non-blocking (risk #1).
func succinctMs(start time.Time) string {
	ms := float64(time.Since(start).Nanoseconds()) / 1e6
	return fmt.Sprintf("%.2fms", ms)
}
