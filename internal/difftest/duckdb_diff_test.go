package difftest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

var acceptDuckdbFlag = flag.Bool("difftest.accept-duckdb", false,
	"rewrite baseline-duckdb.json with the current results instead of asserting")

const (
	goldenDuckdbDir    = "../../testdata/difftest/golden-duckdb"
	baselineDuckdbPath = "../../testdata/difftest/baseline-duckdb.json"
)

// runDuckdbCase rewrites + converts one case and compares to the duckdb golden.
func runDuckdbCase(c Case) (status, detail string) {
	goldenBase := filepath.Join(goldenDuckdbDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case (DuckDB mode)"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden missing; run `make capture-duckdb-golden`"
	}

	actual, status, detail := goRewriteAndConvert(c)
	if status != "" {
		return status, detail
	}

	wantTok, err := Normalize(string(wantSQL))
	if err != nil {
		return "parser-gap", "lex Java golden: " + err.Error()
	}
	gotTok, err := Normalize(actual)
	if err != nil {
		return "parser-gap", "lex Go output: " + err.Error()
	}
	if slices.Equal(wantTok, gotTok) {
		return "pass", ""
	}
	return "fail", firstDiff(wantTok, gotTok)
}

func goRewriteAndConvert(c Case) (sql, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			sql, status, detail = "", "go-error", fmt.Sprintf("panic: %v", r)
		}
	}()
	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		return "", "go-error", err.Error()
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	planned, err := rewrite.Rewrite(c.SQL, ctx, analyzed)
	if err != nil {
		return "", "go-error", err.Error()
	}
	conv := &converter.DuckDBSqlConverter{}
	out, err := conv.Convert(planned, ctx)
	if err != nil {
		return "", "go-error", err.Error()
	}
	return out, "", ""
}

func TestDifferentialDuckDB(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	results := make(map[string]string, len(cases))
	for _, c := range cases {
		s, d := runDuckdbCase(c)
		results[c.ID()] = s
		if s == "pass" {
			t.Logf("PASS  %s", c.ID())
		} else {
			t.Logf("%-12s %s — %s", s, c.ID(), d)
		}
	}

	if *acceptDuckdbFlag {
		writeDuckdbBaseline(t, results)
		return
	}

	base := readDuckdbBaseline(t)
	regr, impr := 0, 0
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			regr++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, st)
		case w != "pass" && st == "pass":
			impr++
			t.Errorf("%s now passes — run `make duckdb-difftest-accept` to update", id)
		}
	}
	t.Logf("\n%s", duckdbSummary(results))
	if regr == 0 && impr == 0 {
		t.Logf("baseline-duckdb 一致, 无回归")
	}
}

func duckdbSummary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("DUCKDB 方言差分计分板: %d/%d 通过", counts["pass"], len(results))
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-12s %d", k, counts[k])
	}
	return s
}

func readDuckdbBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselineDuckdbPath)
	if err != nil {
		t.Fatalf("read duckdb baseline: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse duckdb baseline: %v", err)
	}
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeDuckdbBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, _ := json.MarshalIndent(b, "", "  ")
	if err := os.WriteFile(baselineDuckdbPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("baseline-duckdb 已更新")
}
