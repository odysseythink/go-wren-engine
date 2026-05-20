package service

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

// PreviewService provides preview, dry-plan, and dry-run functionality.
type PreviewService struct {
	metadata     Metadata
	sqlConverter converter.SqlConverter
	configMgr    *config.ConfigManager
}

// Metadata is the interface for query execution metadata.
type Metadata interface {
	DirectQuery(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error)
	DescribeQuery(ctx context.Context, sql string, params []connector.Parameter) ([]connector.Column, error)
	DirectDDL(ctx context.Context, sql string) error
}

// NewPreviewService creates a new PreviewService.
func NewPreviewService(metadata Metadata, sqlConverter converter.SqlConverter, configMgr *config.ConfigManager) *PreviewService {
	return &PreviewService{
		metadata:     metadata,
		sqlConverter: sqlConverter,
		configMgr:    configMgr,
	}
}

// Preview executes a preview query.
func (s *PreviewService) Preview(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string, limit int64) (*dto.PreviewResponse, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog:             wrenMDL.Catalog(),
		Schema:              wrenMDL.Schema(),
		EnableDynamicFields: s.configMgr.EnableDynamicFields(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, fmt.Errorf("rewrite failed: %w", err)
	}
	converted, err := s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("dialect convert failed: %w", err)
	}

	it, err := s.metadata.DirectQuery(ctx, converted, nil)
	if err != nil {
		return nil, fmt.Errorf("execute failed: %w", err)
	}
	defer it.Close()

	cols := it.Columns()
	resp := &dto.PreviewResponse{
		Columns: make([]dto.PreviewColumn, len(cols)),
		Data:    make([][]any, 0, limit),
	}
	for i, c := range cols {
		resp.Columns[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	for it.Next() {
		if int64(len(resp.Data)) >= limit {
			break
		}
		resp.Data = append(resp.Data, it.Get())
	}
	return resp, nil
}

// DryPlan returns the rewritten SQL plan.
func (s *PreviewService) DryPlan(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string, modelingOnly bool) (string, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return "", err
	}
	if modelingOnly {
		return planned, nil
	}
	return s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
}

// DryRun returns the column schema without executing.
func (s *PreviewService) DryRun(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string) ([]dto.PreviewColumn, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, err
	}
	converted, err := s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, err
	}
	cols, err := s.metadata.DescribeQuery(ctx, converted, nil)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PreviewColumn, len(cols))
	for i, c := range cols {
		out[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	return out, nil
}
