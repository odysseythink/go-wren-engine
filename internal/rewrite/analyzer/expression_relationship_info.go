package analyzer

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// ExpressionRelationshipInfo captures the relationship chain within a
// dereference expression. Mirrors analyzer.ExpressionRelationshipInfo.
type ExpressionRelationshipInfo struct {
	qualifiedName           ast.QualifiedName
	relationshipParts       []string
	remainingParts          []string
	relationships           []*dto.Relationship
	relationshipColumnInfos []*RelationshipColumnInfo
}

func newExpressionRelationshipInfo(
	qn ast.QualifiedName,
	infos []*RelationshipColumnInfo,
	remaining []string,
) *ExpressionRelationshipInfo {
	parts := make([]string, len(infos))
	rels := make([]*dto.Relationship, len(infos))
	for i, info := range infos {
		parts[i] = info.column.Name
		rels[i] = info.normalizedRelationship
	}
	return &ExpressionRelationshipInfo{
		qualifiedName:           qn,
		relationshipParts:       parts,
		remainingParts:          remaining,
		relationships:           rels,
		relationshipColumnInfos: infos,
	}
}

func (e *ExpressionRelationshipInfo) QualifiedName() ast.QualifiedName   { return e.qualifiedName }
func (e *ExpressionRelationshipInfo) RemainingParts() []string           { return e.remainingParts }
func (e *ExpressionRelationshipInfo) Relationships() []*dto.Relationship { return e.relationships }
