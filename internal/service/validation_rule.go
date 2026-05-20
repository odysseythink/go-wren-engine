package service

import (
	"context"

	"github.com/wren-engine/wren/internal/mdl"
)

// ValidationStatus mirrors Java ValidationResult.Status (all 5 declared for parity).
type ValidationStatus string

const (
	StatusPass  ValidationStatus = "PASS"
	StatusWarn  ValidationStatus = "WARN"
	StatusError ValidationStatus = "ERROR"
	StatusFail  ValidationStatus = "FAIL"
	StatusSkip  ValidationStatus = "SKIP"
)

// ValidationResult is the wire DTO for a single rule outcome.
// `message` follows Java's @Nullable — omit when absent.
type ValidationResult struct {
	Name     string           `json:"name"`
	Status   ValidationStatus `json:"status"`
	Duration string           `json:"duration"`
	Message  *string          `json:"message,omitempty"`
}

// ValidationRule is the rule contract (Java io.wren.main.validation.ValidationRule).
type ValidationRule interface {
	Validate(ctx context.Context, params map[string]any, analyzed *mdl.AnalyzedMDL) ([]ValidationResult, error)
}
