package service

import (
	"context"
	"errors"
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/connector"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
)

// mockIterator is a no-op RecordIterator.
// Matches the connector.RecordIterator contract: Next() bool, Get() []any,
// Columns() []connector.Column, Close() error.
type mockIterator struct{}

func (m *mockIterator) Next() bool                  { return false }
func (m *mockIterator) Get() []any                  { return nil }
func (m *mockIterator) Columns() []connector.Column { return nil }
func (m *mockIterator) Close() error                { return nil }

type mockMetadata struct {
	directQueryErr error
}

func (m *mockMetadata) DirectQuery(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	if m.directQueryErr != nil {
		return nil, m.directQueryErr
	}
	return &mockIterator{}, nil
}
func (m *mockMetadata) DescribeQuery(ctx context.Context, sql string, params []connector.Parameter) ([]connector.Column, error) {
	return nil, nil
}
func (m *mockMetadata) DirectDDL(ctx context.Context, sql string) error { return nil }

type mockSqlConverter struct{}

func (m *mockSqlConverter) Convert(sql string, ctx *analyzer.SessionContext) (string, error) {
	return sql, nil
}

func newAnalyzedMDL(t *testing.T) *mdl.AnalyzedMDL {
	t.Helper()
	manifest := &dto.Manifest{
		Catalog: "test",
		Schema:  "test",
		Models: []dto.Model{
			{Name: "orders", RefSql: "SELECT 1 AS orderkey", Columns: []dto.Column{
				{Name: "orderkey", Expression: "orderkey", Type: "INTEGER"},
			}},
		},
	}
	return mdl.NewAnalyzedMDL(mdl.WrenMDLFromManifest(manifest))
}

func TestColumnIsValidMissingModel(t *testing.T) {
	rule := &ColumnIsValidRule{metadata: &mockMetadata{}, sqlConverter: &mockSqlConverter{}}
	res, err := rule.Validate(context.Background(), map[string]any{}, newAnalyzedMDL(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusError || res[0].Name != "column_is_valid" {
		t.Fatalf("expected ERROR with name 'column_is_valid', got %+v", res)
	}
}

func TestColumnIsValidMissingColumn(t *testing.T) {
	rule := &ColumnIsValidRule{metadata: &mockMetadata{}, sqlConverter: &mockSqlConverter{}}
	res, err := rule.Validate(context.Background(), map[string]any{"modelName": "orders"}, newAnalyzedMDL(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusError || res[0].Name != "column_is_valid:orders" {
		t.Fatalf("expected ERROR with name 'column_is_valid:orders', got %+v", res)
	}
}

func TestColumnIsValidPass(t *testing.T) {
	rule := &ColumnIsValidRule{metadata: &mockMetadata{}, sqlConverter: &mockSqlConverter{}}
	res, err := rule.Validate(context.Background(),
		map[string]any{"modelName": "orders", "columnName": "orderkey"},
		newAnalyzedMDL(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusPass {
		t.Fatalf("expected PASS, got %+v", res)
	}
	if res[0].Name != "column_is_valid:orders:orderkey" {
		t.Fatalf("unexpected name: %s", res[0].Name)
	}
}

func TestColumnIsValidFailOnDirectQueryError(t *testing.T) {
	rule := &ColumnIsValidRule{
		metadata:     &mockMetadata{directQueryErr: errors.New("Binder Error: no such column 'badcol'")},
		sqlConverter: &mockSqlConverter{},
	}
	res, err := rule.Validate(context.Background(),
		map[string]any{"modelName": "orders", "columnName": "badcol"},
		newAnalyzedMDL(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusFail {
		t.Fatalf("expected FAIL, got %+v", res)
	}
	if res[0].Name != "column_is_valid:orders:badcol" {
		t.Fatalf("unexpected name: %s", res[0].Name)
	}
	if res[0].Message == nil || *res[0].Message == "" {
		t.Fatalf("expected non-empty message, got %+v", res[0].Message)
	}
}
