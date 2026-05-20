package difftest

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/analyzer/decisionpoint"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

var acceptAnalysisFlag = flag.Bool("difftest.accept-analysis", false, "rewrite baseline")

const (
	goldenAnalysisDir    = "../../testdata/difftest/golden-analysis"
	baselineAnalysisPath = "../../testdata/difftest/baseline-analysis.json"
)

func TestAnalysis(t *testing.T) {
	cases, err := LoadCorpus("../../testdata/difftest/cases")
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Skip("no corpus")
	}
	results := make(map[string]string)
	for _, c := range cases {
		status, detail := runAnalysisCase(c)
		results[c.ID()] = status
		if status == "fail" {
			t.Errorf("%s: %s", c.ID(), detail)
		}
	}
	if *acceptAnalysisFlag {
		b, _ := json.MarshalIndent(results, "", "  ")
		_ = os.WriteFile(baselineAnalysisPath, b, 0644)
		t.Log("baseline updated")
		return
	}
	baseline := loadBaseline(baselineAnalysisPath)
	for _, c := range cases {
		want := baseline[c.ID()]
		if want == "" {
			want = "no-golden"
		}
		if got := results[c.ID()]; got != want {
			t.Errorf("%s: baseline %q, got %q", c.ID(), want, got)
		}
	}
}

func runAnalysisCase(c Case) (status, detail string) {
	goldenPath := filepath.Join(goldenAnalysisDir, c.Group, c.Name+".json")
	if _, err := os.Stat(goldenPath + ".error"); err == nil {
		return "oracle-error", "Java returned error"
	}
	wantJSON, err := os.ReadFile(goldenPath)
	if err != nil {
		return "no-golden", "golden missing; run make capture-analysis-golden"
	}

	wrenMDL, err := mdl.WrenMDLFromJSON(string(c.ManifestJSON))
	if err != nil {
		return "fail", "mdl parse: " + err.Error()
	}
	stmt, err := parser.ParseSQL(c.SQL)
	if err != nil {
		return "fail", "parse: " + err.Error()
	}
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyses := decisionpoint.Analyze(stmt, ctx, wrenMDL)
	dtos := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		dtos[i] = a.ToDto()
	}
	gotJSON, _ := json.Marshal(dtos)

	// Risk #15: tolerate ±1 column drift in deeply-nested NodeLocations
	// (`Identifier` token positions differ between ANTLR Go and ANTLR Java
	// because of tokenizer normalization timing).
	if err := JSONEqualWithOptions(wantJSON, gotJSON, CompareOptions{LooseNodeLocation: true}); err != nil {
		return "fail", err.Error()
	}
	return "pass", ""
}

func loadBaseline(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	var b map[string]string
	if err := json.Unmarshal(raw, &b); err != nil {
		return map[string]string{}
	}
	return b
}
