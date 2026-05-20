package service

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/mdl"
)

// ValidationService manages validation rules (Java io.wren.main.ValidationService).
type ValidationService struct {
	rules map[string]ValidationRule
}

// NewValidationService accepts the same two deps Java does: metadata + sqlConverter.
func NewValidationService(metadata Metadata, sqlConverter converter.SqlConverter) *ValidationService {
	return &ValidationService{
		rules: map[string]ValidationRule{
			"column_is_valid": &ColumnIsValidRule{metadata: metadata, sqlConverter: sqlConverter},
		},
	}
}

// Validate dispatches by rule name. Returns Java-style NOT_FOUND-mapped error on unknown rule.
func (s *ValidationService) Validate(ctx context.Context, ruleName string, params map[string]any, analyzed *mdl.AnalyzedMDL) ([]ValidationResult, error) {
	rule, ok := s.rules[ruleName]
	if !ok {
		return nil, fmt.Errorf("Validation rule not found: %s", ruleName)
	}
	if params == nil {
		params = map[string]any{} // risk #10 — Java tolerates null parameters
	}
	return rule.Validate(ctx, params, analyzed)
}
