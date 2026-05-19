package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// dateSpineName is the reserved CTE name for the date spine.
// Mirrors Java DateSpineInfo.NAME.
const dateSpineName = "date_spine"

// DateSpineInfo is a QueryDescriptor for the date-spine CTE.
// Mirrors Java io.wren.base.sqlrewrite.DateSpineInfo.
type DateSpineInfo struct {
	query *ast.Query
}

// dateSpineInfoGet builds a DateSpineInfo. Mirrors DateSpineInfo.get.
func dateSpineInfoGet(ds dto.DateSpine) (*DateSpineInfo, error) {
	query, err := createDateSpineQuery(ds)
	if err != nil {
		return nil, err
	}
	return &DateSpineInfo{query: query}, nil
}

func (d *DateSpineInfo) Name() string              { return dateSpineName }
func (d *DateSpineInfo) RequiredObjects() []string { return nil }
func (d *DateSpineInfo) Query() *ast.Query         { return d.query }
