package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// RelationInfo is a QueryDescriptor backed by a rendered model query.
// Mirrors Java RelationInfo.
type RelationInfo struct {
	relationable    *dto.Model
	requiredObjects []string
	query           *ast.Query
}

func newRelationInfo(model *dto.Model, requiredObjects []string, query *ast.Query) *RelationInfo {
	return &RelationInfo{relationable: model, requiredObjects: requiredObjects, query: query}
}

func (r *RelationInfo) Name() string              { return r.relationable.Name }
func (r *RelationInfo) RequiredObjects() []string  { return r.requiredObjects }
func (r *RelationInfo) Query() *ast.Query          { return r.query }

// relationInfoOfModel renders a model into a RelationInfo. Mirrors
// RelationInfo.get(Relationable, WrenMDL) for the Model case.
func relationInfoOfModel(model *dto.Model, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	r, err := newModelSqlRender(model, wrenMDL)
	if err != nil {
		return nil, err
	}
	return r.render()
}
