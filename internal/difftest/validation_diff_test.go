package difftest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

const (
	goldenValidationDir    = "../../testdata/difftest/golden-validation"
	baselineValidationPath = "../../testdata/difftest/baseline-validation.json"
)

func TestValidation(t *testing.T) {
	metadata := duckdb.NewMetadata()
	defer metadata.Close()
	sqlConverter := &converter.DuckDBSqlConverter{}
	vs := service.NewValidationService(metadata, sqlConverter)

	cases := []struct {
		id       string
		manifest *dto.Manifest
		params   map[string]any
	}{
		{
			id: "validation/pass",
			manifest: &dto.Manifest{Catalog: "test", Schema: "test", Models: []dto.Model{
				{Name: "orders", RefSql: "SELECT 1 AS orderkey", Columns: []dto.Column{
					{Name: "orderkey", Expression: "orderkey", Type: "INTEGER"},
				}},
			}},
			params: map[string]any{"modelName": "orders", "columnName": "orderkey"},
		},
		{
			id: "validation/fail",
			manifest: &dto.Manifest{Catalog: "test", Schema: "test", Models: []dto.Model{
				{Name: "orders", RefSql: "SELECT 1 AS orderkey", Columns: []dto.Column{
					{Name: "badcol", Expression: "badcol", Type: "INTEGER"},
				}},
			}},
			params: map[string]any{"modelName": "orders", "columnName": "badcol"},
		},
		{
			id:       "validation/error_missing_model",
			manifest: &dto.Manifest{Catalog: "test", Schema: "test", Models: []dto.Model{}},
			params:   map[string]any{"modelName": "", "columnName": "x"},
		},
		{
			id: "validation/error_missing_column",
			manifest: &dto.Manifest{Catalog: "test", Schema: "test", Models: []dto.Model{
				{Name: "orders", RefSql: "SELECT 1", Columns: []dto.Column{}},
			}},
			params: map[string]any{"modelName": "orders", "columnName": ""},
		},
	}

	results := map[string]string{}
	for _, c := range cases {
		analyzed := mdl.NewAnalyzedMDL(mdl.WrenMDLFromManifest(c.manifest))
		got, err := vs.Validate(context.Background(), "column_is_valid", c.params, analyzed)
		if err != nil {
			results[c.id] = "fail"
			t.Logf("%s: service error: %v", c.id, err)
			continue
		}
		gotJSON, _ := json.Marshal(got)

		wantJSON, err := os.ReadFile(filepath.Join(goldenValidationDir, filepath.Base(c.id)+".json"))
		if err != nil {
			results[c.id] = "no-golden"
			continue
		}

		// Risk #1: duration is non-deterministic — mask on both sides.
		gotMasked, _ := maskDuration(gotJSON)
		wantMasked, _ := maskDuration(wantJSON)

		if err := JSONEqualWithOptions(wantMasked, gotMasked, CompareOptions{}); err != nil {
			results[c.id] = "fail"
			t.Logf("%s mismatch: %v", c.id, err)
		} else {
			results[c.id] = "pass"
		}
	}

	baseline := loadBaseline(baselineValidationPath)
	for id, got := range results {
		want := baseline[id]
		if want == "" {
			want = "no-golden"
		}
		if got != want {
			t.Errorf("%s: baseline %q, got %q", id, want, got)
		}
	}
}

func maskDuration(b []byte) ([]byte, error) {
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}
	for i := range arr {
		delete(arr[i], "duration")
	}
	return json.Marshal(arr)
}
