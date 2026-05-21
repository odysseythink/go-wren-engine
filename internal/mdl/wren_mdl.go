package mdl

import (
	"encoding/json"
	"fmt"

	"github.com/wren-engine/wren/internal/dto"
)

// WrenMDL is the central MDL representation.
type WrenMDL struct {
	catalog           string
	schema            string
	manifest          *dto.Manifest
	models            map[string]*dto.Model
	metrics           map[string]*dto.Metric
	cumulativeMetrics map[string]*dto.CumulativeMetric
	relationships     map[string]*dto.Relationship
	enumDefinitions   map[string]*dto.EnumDefinition
	views             map[string]*dto.View
	macros            map[string]*dto.Macro
}

// WrenMDLFromManifest creates a WrenMDL from a Manifest.
func WrenMDLFromManifest(manifest *dto.Manifest) *WrenMDL {
	manifest = RenderJinja(manifest)
	m := &WrenMDL{
		catalog:           manifest.Catalog,
		schema:            manifest.Schema,
		manifest:          manifest,
		models:            make(map[string]*dto.Model),
		metrics:           make(map[string]*dto.Metric),
		cumulativeMetrics: make(map[string]*dto.CumulativeMetric),
		relationships:     make(map[string]*dto.Relationship),
		enumDefinitions:   make(map[string]*dto.EnumDefinition),
		views:             make(map[string]*dto.View),
		macros:            make(map[string]*dto.Macro),
	}
	for i := range manifest.Models {
		m.models[manifest.Models[i].Name] = &manifest.Models[i]
	}
	for i := range manifest.Metrics {
		m.metrics[manifest.Metrics[i].Name] = &manifest.Metrics[i]
	}
	for i := range manifest.CumulativeMetrics {
		m.cumulativeMetrics[manifest.CumulativeMetrics[i].Name] = &manifest.CumulativeMetrics[i]
	}
	for i := range manifest.Relationships {
		m.relationships[manifest.Relationships[i].Name] = &manifest.Relationships[i]
	}
	for i := range manifest.EnumDefinitions {
		m.enumDefinitions[manifest.EnumDefinitions[i].Name] = &manifest.EnumDefinitions[i]
	}
	for i := range manifest.Views {
		m.views[manifest.Views[i].Name] = &manifest.Views[i]
	}
	for i := range manifest.Macros {
		m.macros[manifest.Macros[i].Name] = &manifest.Macros[i]
	}
	return m
}

// WrenMDLFromJSON creates a WrenMDL from a JSON string.
func WrenMDLFromJSON(jsonStr string) (*WrenMDL, error) {
	var manifest dto.Manifest
	if err := json.Unmarshal([]byte(jsonStr), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}
	return WrenMDLFromManifest(&manifest), nil
}

// Catalog returns the catalog.
func (m *WrenMDL) Catalog() string { return m.catalog }

// Schema returns the schema.
func (m *WrenMDL) Schema() string { return m.schema }

// Manifest returns the underlying manifest.
func (m *WrenMDL) Manifest() *dto.Manifest { return m.manifest }

// GetModel returns a model by name.
func (m *WrenMDL) GetModel(name string) (*dto.Model, bool) {
	model, ok := m.models[name]
	return model, ok
}

// ListModels returns all models.
func (m *WrenMDL) ListModels() []*dto.Model {
	result := make([]*dto.Model, 0, len(m.models))
	for _, model := range m.models {
		result = append(result, model)
	}
	return result
}

// GetMetric returns a metric by name.
func (m *WrenMDL) GetMetric(name string) (*dto.Metric, bool) {
	metric, ok := m.metrics[name]
	return metric, ok
}

// GetCumulativeMetric returns a cumulative metric by name.
func (m *WrenMDL) GetCumulativeMetric(name string) (*dto.CumulativeMetric, bool) {
	cm, ok := m.cumulativeMetrics[name]
	return cm, ok
}

// GetRelationshipColumn returns model's column named columnName if it is a
// relationship column. Mirrors Java WrenMDL.getRelationshipColumn.
func GetRelationshipColumn(model *dto.Model, columnName string) (*dto.Column, bool) {
	for i := range model.Columns {
		c := &model.Columns[i]
		if c.Name == columnName && c.Relationship != "" {
			return c, true
		}
	}
	return nil, false
}

// GetRelationship returns a relationship by name.
func (m *WrenMDL) GetRelationship(name string) (*dto.Relationship, bool) {
	r, ok := m.relationships[name]
	return r, ok
}

// GetEnumDefinition returns an enum definition by name.
func (m *WrenMDL) GetEnumDefinition(name string) (*dto.EnumDefinition, bool) {
	e, ok := m.enumDefinitions[name]
	return e, ok
}

// GetView returns a view by name.
func (m *WrenMDL) GetView(name string) (*dto.View, bool) {
	v, ok := m.views[name]
	return v, ok
}

// GetMacro returns a macro by name.
func (m *WrenMDL) GetMacro(name string) (*dto.Macro, bool) {
	macro, ok := m.macros[name]
	return macro, ok
}

// IsObjectExist checks if a model, metric, view, or cumulative metric exists.
func (m *WrenMDL) IsObjectExist(name string) bool {
	_, ok := m.models[name]
	if ok {
		return true
	}
	_, ok = m.metrics[name]
	if ok {
		return true
	}
	_, ok = m.cumulativeMetrics[name]
	if ok {
		return true
	}
	_, ok = m.views[name]
	return ok
}

// GetDateSpine returns the manifest's date spine (defaulted at manifest load).
// Mirrors Java WrenMDL.getDateSpine.
func (m *WrenMDL) GetDateSpine() dto.DateSpine {
	return m.manifest.DateSpine
}
