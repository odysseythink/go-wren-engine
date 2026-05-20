package analyzer

import (
	"github.com/wren-engine/wren/internal/dto"
)

// RelationshipColumnInfo pairs a relationship column with its normalized
// relationship. Mirrors analyzer.RelationshipColumnInfo.
type RelationshipColumnInfo struct {
	column                 *dto.Column
	model                  *dto.Model
	normalizedRelationship *dto.Relationship
}

func NewRelationshipColumnInfo(model *dto.Model, column *dto.Column, rel *dto.Relationship) *RelationshipColumnInfo {
	norm := rel
	if rel.Models[1] != column.Type {
		norm = dto.ReverseRelationship(rel)
	}
	return &RelationshipColumnInfo{column: column, model: model, normalizedRelationship: norm}
}

func (r *RelationshipColumnInfo) NormalizedRelationship() *dto.Relationship {
	return r.normalizedRelationship
}
