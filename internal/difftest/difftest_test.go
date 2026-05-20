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
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

var acceptFlag = flag.Bool("difftest.accept", false,
	"rewrite baseline.json with the current results instead of asserting")

const (
	casesDir     = "../../testdata/difftest/cases"
	goldenDir    = "../../testdata/difftest/golden"
	baselinePath = "../../testdata/difftest/baseline.json"
)

type baselineFile struct {
	Version int               `json:"version"`
	Cases   map[string]string `json:"cases"`
}

// runCase rewrites one case with the Go engine and compares it to the frozen
// Java golden. It returns one of: pass, fail, parser-gap, go-error,
// oracle-error, oracle-error-permanent, no-golden.
func runCase(c Case) (status string, detail string) {
	goldenBase := filepath.Join(goldenDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java engine returned a permanent error for this case (known Java bug)"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden file missing; run `make capture-golden`"
	}

	actual, status, detail := goRewrite(c)
	if status != "" {
		return status, detail
	}

	wantTokens, err := Normalize(string(wantSQL))
	if err != nil {
		return "parser-gap", "cannot lex Java golden: " + err.Error()
	}
	gotTokens, err := Normalize(actual)
	if err != nil {
		return "parser-gap", "cannot lex Go output: " + err.Error()
	}
	if slices.Equal(wantTokens, gotTokens) {
		return "pass", ""
	}
	return "fail", firstDiff(wantTokens, gotTokens)
}

// goRewrite runs the Go rewrite engine, recovering panics. On success it
// returns (sql, "", ""); on failure ("", "go-error", detail).
func goRewrite(c Case) (sql, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			sql, status, detail = "", "go-error", fmt.Sprintf("panic: %v", r)
		}
	}()
	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		return "", "go-error", "unmarshal manifest: " + err.Error()
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	out, err := rewrite.Rewrite(c.SQL, ctx, analyzed)
	if err != nil {
		return "", "go-error", err.Error()
	}
	return out, "", ""
}

func firstDiff(want, got []string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("token %d: want %q, got %q", i, want[i], got[i])
		}
	}
	return fmt.Sprintf("token count: want %d, got %d", len(want), len(got))
}

func TestDifferential(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	results := make(map[string]string, len(cases))
	for _, c := range cases {
		status, detail := runCase(c)
		results[c.ID()] = status
		if status == "pass" {
			t.Logf("PASS  %s", c.ID())
		} else {
			t.Logf("%-12s %s — %s", status, c.ID(), detail)
		}
	}

	if *acceptFlag {
		writeBaseline(t, results)
		return
	}

	base := readBaseline(t)
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id] // missing => "" => treated as non-pass
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want == "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes (was oracle-error-permanent) — run `make difftest-accept`", id)
		case want != "pass" && want != "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make difftest-accept` to update baseline", id)
		}
	}

	t.Logf("\n%s", summary(results))
	if regressions == 0 && improvements == 0 {
		t.Logf("baseline 一致, 无回归")
	}
}

func summary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	denominator := len(results) - counts["oracle-error-permanent"]
	s := fmt.Sprintf("差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}

func readBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	if err := os.WriteFile(baselinePath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	t.Logf("baseline 已更新: %s", baselinePath)
}
