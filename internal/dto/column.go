package dto

import "fmt"

// Column represents a column in a model or metric.
type Column struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Relationship string            `json:"relationship,omitempty"`
	IsCalculated bool              `json:"isCalculated"`
	NotNull      bool              `json:"notNull"`
	Expression   string            `json:"expression,omitempty"`
	Properties   map[string]string `json:"properties,omitempty"`
}

// GetExpression returns the expression or the quoted name as default.
func (c Column) GetExpression() string {
	if c.Expression != "" {
		return c.Expression
	}
	return fmt.Sprintf(`"%s"`, c.Name)
}
