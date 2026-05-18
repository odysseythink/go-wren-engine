package dto

import "strings"

// Macro represents a reusable macro definition.
type Macro struct {
	Name       string            `json:"name"`
	Definition string            `json:"definition"`
	Properties map[string]string `json:"properties,omitempty"`
}

// GetBody extracts the body from the definition (after =>).
func (m Macro) GetBody() string {
	parts := strings.SplitN(m.Definition, "=>", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// GetParameters extracts parameter names from the definition (before =>).
func (m Macro) GetParameters() []string {
	parts := strings.SplitN(m.Definition, "=>", 2)
	if len(parts) == 2 {
		paramStr := strings.TrimSpace(parts[0])
		if paramStr == "" {
			return nil
		}
		params := strings.Split(paramStr, ",")
		for i := range params {
			params[i] = strings.TrimSpace(params[i])
		}
		return params
	}
	return nil
}
