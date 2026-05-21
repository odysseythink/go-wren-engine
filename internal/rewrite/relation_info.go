package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// RelationInfo is a QueryDescriptor backed by a rendered model/metric query.
type RelationInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

func newRelationInfo(name string, requiredObjects []string, query *ast.Query) *RelationInfo {
	return &RelationInfo{name: name, requiredObjects: requiredObjects, query: query}
}

func (r *RelationInfo) Name() string              { return r.name }
func (r *RelationInfo) RequiredObjects() []string { return r.requiredObjects }
func (r *RelationInfo) Query() *ast.Query         { return r.query }

// relationInfoOfModel renders a full model (static path).
func relationInfoOfModel(model *dto.Model, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	r, err := newModelSqlRender(model, wrenMDL)
	if err != nil {
		return nil, err
	}
	return r.render()
}

// relationInfoOfModelWithFields renders a pruned model selecting only requiredFields.
func relationInfoOfModelWithFields(model *dto.Model, wrenMDL *mdl.WrenMDL, requiredFields []string) (*RelationInfo, error) {
	r, err := newModelSqlRenderWithFields(model, wrenMDL, requiredFields)
	if err != nil {
		return nil, err
	}
	return r.render()
}

// relationInfoOfMetric renders a full metric (static path).
func relationInfoOfMetric(metric *dto.Metric, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	return newMetricSqlRender(metric, wrenMDL).render()
}

// relationInfoOfMetricWithFields renders a pruned metric selecting only requiredFields.
func relationInfoOfMetricWithFields(metric *dto.Metric, wrenMDL *mdl.WrenMDL, requiredFields []string) (*RelationInfo, error) {
	r := newMetricSqlRenderWithFields(metric, wrenMDL, requiredFields)
	return r.render()
}
