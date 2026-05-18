package dto

import "encoding/json"

// Manifest is the top-level container for the MDL.
type Manifest struct {
	Catalog           string             `json:"catalog"`
	Schema            string             `json:"schema"`
	Models            []Model            `json:"models"`
	Relationships     []Relationship     `json:"relationships"`
	EnumDefinitions   []EnumDefinition   `json:"enumDefinitions"`
	Metrics           []Metric           `json:"metrics"`
	CumulativeMetrics []CumulativeMetric `json:"cumulativeMetrics"`
	Views             []View             `json:"views"`
	Macros            []Macro            `json:"macros"`
	DateSpine         DateSpine          `json:"dateSpine"`
}

// UnmarshalJSON provides default empty slices for nil arrays.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type Alias Manifest
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if m.Models == nil {
		m.Models = []Model{}
	}
	if m.Relationships == nil {
		m.Relationships = []Relationship{}
	}
	if m.EnumDefinitions == nil {
		m.EnumDefinitions = []EnumDefinition{}
	}
	if m.Metrics == nil {
		m.Metrics = []Metric{}
	}
	if m.CumulativeMetrics == nil {
		m.CumulativeMetrics = []CumulativeMetric{}
	}
	if m.Views == nil {
		m.Views = []View{}
	}
	if m.Macros == nil {
		m.Macros = []Macro{}
	}
	if m.DateSpine.Unit == "" {
		m.DateSpine = DefaultDateSpine()
	}
	return nil
}
