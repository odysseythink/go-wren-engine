package service

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/mdl"
)

// ColumnIsValidRule (stub — implementation lands in Task 3.1).
type ColumnIsValidRule struct {
	metadata     Metadata
	sqlConverter converter.SqlConverter
}

func (r *ColumnIsValidRule) Validate(ctx context.Context, params map[string]any, analyzed *mdl.AnalyzedMDL) ([]ValidationResult, error) {
	return nil, fmt.Errorf("ColumnIsValid not yet implemented (Task 3.1)")
}
