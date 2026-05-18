package service

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
)

// ValidationResult represents the result of a validation rule.
type ValidationResult struct {
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
}

// ValidationRule validates a specific aspect of the MDL.
type ValidationRule interface {
	Validate(ctx context.Context, params map[string]any, mdl *mdl.AnalyzedMDL) ([]ValidationResult, error)
}

// ValidationService manages validation rules.
type ValidationService struct {
	rules map[string]ValidationRule
}

// NewValidationService creates a new ValidationService.
func NewValidationService() *ValidationService {
	return &ValidationService{
		rules: map[string]ValidationRule{
			"column_is_valid": &ColumnIsValidRule{},
		},
	}
}

// Validate runs a validation rule by name.
func (s *ValidationService) Validate(ctx context.Context, ruleName string, params map[string]any, mdl *mdl.AnalyzedMDL) ([]ValidationResult, error) {
	rule, ok := s.rules[ruleName]
	if !ok {
		return nil, fmt.Errorf("unknown validation rule: %s", ruleName)
	}
	return rule.Validate(ctx, params, mdl)
}

// ColumnIsValidRule validates that columns exist.
type ColumnIsValidRule struct{}

func (r *ColumnIsValidRule) Validate(ctx context.Context, params map[string]any, mdl *mdl.AnalyzedMDL) ([]ValidationResult, error) {
	// TODO: Implement column validation
	return []ValidationResult{{Name: "column_is_valid", Valid: true}}, nil
}
