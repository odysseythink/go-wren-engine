package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// tableNameToQualifiedName converts a CatalogSchemaTableName to a QualifiedName.
func tableNameToQualifiedName(cstn CatalogSchemaTableName) ast.QualifiedName {
	parts := []string{}
	if cstn.Catalog != "" {
		parts = append(parts, cstn.Catalog)
	}
	if cstn.Schema != "" {
		parts = append(parts, cstn.Schema)
	}
	parts = append(parts, cstn.Table)
	return ast.QualifiedNameOf(parts...)
}

// hasSuffix reports whether qn ends with suffix (by Parts comparison).
func hasSuffix(qn, suffix ast.QualifiedName) bool {
	if len(suffix.Parts) > len(qn.Parts) {
		return false
	}
	offset := len(qn.Parts) - len(suffix.Parts)
	for i, p := range suffix.Parts {
		if !strings.EqualFold(p, qn.Parts[offset+i]) {
			return false
		}
	}
	return true
}

// qualifiedNamePrefix returns all but the last part of a QualifiedName,
// or nil if there are fewer than 2 parts.
func qualifiedNamePrefix(qn *ast.QualifiedName) *ast.QualifiedName {
	if qn == nil || len(qn.Parts) < 2 {
		return nil
	}
	prefix := ast.QualifiedName{
		Parts:         qn.Parts[:len(qn.Parts)-1],
		OriginalParts: qn.OriginalParts[:len(qn.OriginalParts)-1],
	}
	return &prefix
}

// qualifiedNameSuffix returns the last part of a QualifiedName.
func qualifiedNameSuffix(qn *ast.QualifiedName) string {
	if qn == nil || len(qn.Parts) == 0 {
		return ""
	}
	return qn.Parts[len(qn.Parts)-1]
}

// qualifiedNameOfExpression extracts a QualifiedName from an expression chain.
// Mirrors trino QueryUtil.getQualifiedName.
func qualifiedNameOfExpression(expr ast.Expression) *ast.QualifiedName {
	return ast.GetQualifiedName(expr)
}

func errTooManyDots(name ast.QualifiedName) error {
	return fmt.Errorf("too many dots in name: %s", name.String())
}

// toCatalogSchemaTableName resolves a (≤3-part) QualifiedName to a CSTN,
// filling missing parts from the session. Mirrors Utils.toCatalogSchemaTableName.
func toCatalogSchemaTableName(ctx *base.SessionContext, name ast.QualifiedName) (CatalogSchemaTableName, error) {
	parts := name.Parts
	if len(parts) > 3 {
		return CatalogSchemaTableName{}, errTooManyDots(name)
	}
	// reversed: parts[last] is the object name
	obj := parts[len(parts)-1]
	schema := ctx.Schema
	if len(parts) > 1 {
		schema = parts[len(parts)-2]
	}
	catalog := ctx.Catalog
	if len(parts) > 2 {
		catalog = parts[len(parts)-3]
	}
	return CatalogSchemaTableName{Catalog: catalog, Schema: schema, Table: obj}, nil
}

// sortedModels returns models sorted by name.
func sortedModels(wrenMDL *mdl.WrenMDL) []*dto.Model {
	models := wrenMDL.ListModels()
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models
}

// sortedMetrics returns metrics sorted by name.
func sortedMetrics(wrenMDL *mdl.WrenMDL) []*dto.Metric {
	manifest := wrenMDL.Manifest()
	metrics := make([]*dto.Metric, len(manifest.Metrics))
	for i := range manifest.Metrics {
		metrics[i] = &manifest.Metrics[i]
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Name < metrics[j].Name })
	return metrics
}

// usedContains reports whether the used relations contain the given model name.
func usedContains(used []Relation, modelName string) bool {
	for _, r := range used {
		if r.Name == modelName {
			return true
		}
	}
	return false
}

// toField creates a Field for a model column. Mirrors Utils.toField.
func toField(wrenMDL *mdl.WrenMDL, modelName string, column *dto.Column, used []Relation) *Field {
	name := column.Name
	return &Field{
		tableName:         CatalogSchemaTableName{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema(), Table: modelName},
		columnName:        name,
		name:              &name,
		sourceDatasetName: &modelName,
		sourceColumn:      column,
	}
}

// AnalyzeFrom builds a Scope for a FROM relation. Mirrors Utils.analyzeFrom.
// Called by ScopeAnalyzer (indirectly via AnalyzeScope); P3b will add metric
// dimension/measure fields here.
func AnalyzeFrom(wrenMDL *mdl.WrenMDL, ctx *base.SessionContext, node ast.Relation, parent *Scope) *Scope {
	scopeAnalysis := AnalyzeScope(wrenMDL, node, ctx)
	used := scopeAnalysis.UsedWrenObjects()
	var fields []*Field
	for _, model := range sortedModels(wrenMDL) {
		if !usedContains(used, model.Name) {
			continue
		}
		for i := range model.Columns {
			fields = append(fields, toField(wrenMDL, model.Name, &model.Columns[i], used))
		}
	}
	for _, metric := range sortedMetrics(wrenMDL) {
		if !usedContains(used, metric.Name) {
			continue
		}
		for i := range metric.Dimension {
			fields = append(fields, toField(wrenMDL, metric.Name, &metric.Dimension[i], used))
		}
		for i := range metric.Measure {
			fields = append(fields, toField(wrenMDL, metric.Name, &metric.Measure[i], used))
		}
	}
	return ScopeBuilderWithParent(parent).RelationType(NewRelationType(fields)).Build()
}
