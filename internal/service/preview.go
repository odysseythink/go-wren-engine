package service

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector"
	"github.com/wren-engine/wren/internal/converter"
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

// QueryResultDto represents query results.
type QueryResultDto struct {
	Columns []connector.Column `json:"columns"`
	Data    [][]any            `json:"data"`
}

// Preview executes a preview query.
func (s *PreviewService) Preview(ctx context.Context, wrenMDL *mdl.WrenMDL, sql string, limit int64) (*QueryResultDto, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog:             wrenMDL.Catalog(),
		Schema:              wrenMDL.Schema(),
		EnableDynamicFields: s.configMgr.Get().Wren.EnableDynamicFields,
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	plannedSQL, err := rewrite.Rewrite(sql, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, fmt.Errorf("rewrite failed: %w", err)
	}
	convertedSQL, err := s.sqlConverter.Convert(plannedSQL, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("dialect convert failed: %w", err)
	}
	// TODO: Execute query and return results
	_ = convertedSQL
	return &QueryResultDto{Columns: []connector.Column{}, Data: [][]any{}}, nil
}

// DryPlan returns the rewritten SQL plan.
func (s *PreviewService) DryPlan(ctx context.Context, wrenMDL *mdl.WrenMDL, sql string, modelingOnly bool) (string, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	return rewrite.Rewrite(sql, analyzer.GetSessionContext(ctx), analyzed)
}

// DryRun returns the column schema without executing.
func (s *PreviewService) DryRun(ctx context.Context, wrenMDL *mdl.WrenMDL, sql string) ([]connector.Column, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	plannedSQL, err := rewrite.Rewrite(sql, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, err
	}
	convertedSQL, err := s.sqlConverter.Convert(plannedSQL, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, err
	}
	return s.metadata.DescribeQuery(ctx, convertedSQL, nil)
}
