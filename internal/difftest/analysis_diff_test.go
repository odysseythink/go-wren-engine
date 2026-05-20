package difftest

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
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
	return "no-golden", "not yet implemented"
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
