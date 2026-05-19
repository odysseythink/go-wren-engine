package analyzer

import (
	"fmt"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// GetRelationships collects to-1 and to-N relationships used in expr.
// Mirrors ExpressionRelationshipAnalyzer.getRelationships.
func GetRelationships(expr ast.Expression, wrenMDL *mdl.WrenMDL, model *dto.Model) ([]*ExpressionRelationshipInfo, error) {
	c := &relationshipCollector{wrenMDL: wrenMDL, model: model, allowToMany: true}
	if err := c.process(expr); err != nil {
		return nil, err
	}
	return c.infos, nil
}

// GetToOneRelationships collects only to-one relationships; errors on to-many.
// Mirrors ExpressionRelationshipAnalyzer.getToOneRelationships.
func GetToOneRelationships(expr ast.Expression, wrenMDL *mdl.WrenMDL, model *dto.Model) ([]*ExpressionRelationshipInfo, error) {
	c := &relationshipCollector{wrenMDL: wrenMDL, model: model, allowToMany: false}
	if err := c.process(expr); err != nil {
		return nil, err
	}
	return c.infos, nil
}

type relationshipCollector struct {
	wrenMDL     *mdl.WrenMDL
	model       *dto.Model
	allowToMany bool
	infos       []*ExpressionRelationshipInfo
}

func (c *relationshipCollector) process(node ast.Node) error {
	switch n := node.(type) {
	case *ast.DereferenceExpression:
		qn := ast.GetQualifiedName(n)
		if qn != nil {
			info, err := c.createRelationshipInfo(*qn)
			if err != nil {
				return err
			}
			if info != nil {
				c.infos = append(c.infos, info)
				return nil // don't descend into base
			}
		}
		if err := c.process(n.Base); err != nil {
			return err
		}
		return nil
	}
	for _, child := range node.GetChildren() {
		if err := c.process(child); err != nil {
			return err
		}
	}
	return nil
}

func (c *relationshipCollector) createRelationshipInfo(qn ast.QualifiedName) (*ExpressionRelationshipInfo, error) {
	var infos []*RelationshipColumnInfo
	currentModel := c.model
	seen := map[string]bool{}

	for i, part := range qn.Parts {
		col, ok := mdl.GetRelationshipColumn(currentModel, part)
		if !ok {
			if i > 0 {
				return newExpressionRelationshipInfo(qn, infos, qn.Parts[i:]), nil
			}
			return nil, nil
		}

		rel, ok := c.wrenMDL.GetRelationship(col.Relationship)
		if !ok {
			return nil, fmt.Errorf("relationship %q not found", col.Relationship)
		}

		info := NewRelationshipColumnInfo(currentModel, col, rel)
		infos = append(infos, info)

		if !c.allowToMany && dto.IsToMany(info.NormalizedRelationship().JoinType) {
			return nil, fmt.Errorf("to-many relationship %q not allowed here", rel.Name)
		}

		nextModel, ok := c.wrenMDL.GetModel(col.Type)
		if !ok {
			return nil, fmt.Errorf("model %q not found", col.Type)
		}

		if seen[nextModel.Name] {
			return nil, fmt.Errorf("cycle detected in relationship chain")
		}
		seen[nextModel.Name] = true
		currentModel = nextModel
	}

	// All parts were relationships
	return newExpressionRelationshipInfo(qn, infos, nil), nil
}
