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
	if len(parts) != 2 {
		return nil
	}
	paramStr := strings.TrimSpace(parts[0])
	if paramStr == "" {
		return nil
	}
	// Extract "(...)" part, e.g. "add_one(x, y)" -> "x, y"
	start := strings.Index(paramStr, "(")
	end := strings.LastIndex(paramStr, ")")
	if start != -1 && end != -1 && start < end {
		inner := strings.TrimSpace(paramStr[start+1 : end])
		if inner == "" {
			return nil
		}
		params := strings.Split(inner, ",")
		for i := range params {
			params[i] = strings.TrimSpace(params[i])
		}
		return params
	}
	// No parentheses — entire left side is the parameter list (fallback).
	params := strings.Split(paramStr, ",")
	for i := range params {
		params[i] = strings.TrimSpace(params[i])
	}
	return params
}
