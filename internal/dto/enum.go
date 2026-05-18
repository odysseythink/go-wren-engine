package dto

// EnumValue represents a single value in an enum definition.
type EnumValue struct {
	Name       string            `json:"name"`
	Value      string            `json:"value,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// GetValue returns the value or name if value is empty.
func (e EnumValue) GetValue() string {
	if e.Value != "" {
		return e.Value
	}
	return e.Name
}

// EnumDefinition represents an enum definition.
type EnumDefinition struct {
	Name       string            `json:"name"`
	Values     []EnumValue       `json:"values"`
	Properties map[string]string `json:"properties,omitempty"`
}

// ValueOf finds an enum value by name.
func (e EnumDefinition) ValueOf(name string) *EnumValue {
	for _, v := range e.Values {
		if v.Name == name {
			return &v
		}
	}
	return nil
}
