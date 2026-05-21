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

var acceptDynamicFlag = flag.Bool("difftest.accept-dynamic", false,
	"rewrite baseline-dynamic.json with the current results instead of asserting")

const (
	goldenDynamicDir    = "../../testdata/difftest/golden-dynamic"
	baselineDynamicPath = "../../testdata/difftest/baseline-dynamic.json"
)

func runCaseDynamic(c Case) (status string, detail string) {
	goldenBase := filepath.Join(goldenDynamicDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java engine returned a permanent error for this case (known Java bug)"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden file missing; run `make capture-dynamic-golden`"
	}

	actual, status, detail := goRewriteDynamic(c)
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

func goRewriteDynamic(c Case) (sql, status, detail string) {
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
	ctx := &analyzer.SessionContext{
		Catalog:             wrenMDL.Catalog(),
		Schema:              wrenMDL.Schema(),
		EnableDynamicFields: true,
	}
	out, err := rewrite.Rewrite(c.SQL, ctx, analyzed)
	if err != nil {
		return "", "go-error", err.Error()
	}
	return out, "", ""
}

func TestDifferentialDynamic(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	results := make(map[string]string, len(cases))
	for _, c := range cases {
		status, detail := runCaseDynamic(c)
		results[c.ID()] = status
		if status == "pass" {
			t.Logf("PASS  %s", c.ID())
		} else {
			t.Logf("%-12s %s — %s", status, c.ID(), detail)
		}
	}

	if *acceptDynamicFlag {
		writeBaselineDynamic(t, results)
		return
	}

	base := readBaselineDynamic(t)
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id]
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want == "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes (was oracle-error-permanent) — run `make difftest-accept-dynamic`", id)
		case want != "pass" && want != "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make difftest-accept-dynamic` to update baseline", id)
		}
	}

	t.Logf("\n%s", summaryDynamic(results))
	if regressions == 0 && improvements == 0 {
		t.Logf("baseline-dynamic 一致, 无回归")
	}
}

func summaryDynamic(results map[string]string) string {
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
	s := fmt.Sprintf("dynamic 差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}

func readBaselineDynamic(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselineDynamicPath)
	if err != nil {
		t.Fatalf("read baseline-dynamic: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse baseline-dynamic: %v", err)
	}
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeBaselineDynamic(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline-dynamic: %v", err)
	}
	if err := os.WriteFile(baselineDynamicPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write baseline-dynamic: %v", err)
	}
	t.Logf("baseline-dynamic 已更新: %s", baselineDynamicPath)
}
