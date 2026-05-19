package formatter

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/parser"
)

var acceptFlag = flag.Bool("format.accept", false,
	"rewrite baseline.json with current results instead of asserting")

const (
	casesDir     = "../../../testdata/format/cases"
	goldenDir    = "../../../testdata/format/golden"
	baselinePath = "../../../testdata/format/baseline.json"
)

type baselineFile struct {
	Version int               `json:"version"`
	Cases   map[string]string `json:"cases"`
}

// caseID is the cases-dir-relative path of a .sql file, e.g. "slice1/cast.sql".
func collectCases(t *testing.T) []string {
	t.Helper()
	var ids []string
	err := filepath.WalkDir(casesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			rel, _ := filepath.Rel(casesDir, path)
			ids = append(ids, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cases dir: %v", err)
	}
	sort.Strings(ids)
	return ids
}

// runFormatCase formats one case with the Go engine and compares it to the
// Java golden. Status is one of: pass, fail, panic, parse-error, oracle-error,
// no-golden.
func runFormatCase(id string) (status, detail string) {
	goldenBase := filepath.Join(goldenDir, id+".golden")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java parser/formatter rejected this case"
	}
	want, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden missing; run `make capture-format-golden`"
	}
	sqlBytes, err := os.ReadFile(filepath.Join(casesDir, id))
	if err != nil {
		return "no-golden", "case file missing: " + err.Error()
	}

	got, status, detail := goFormat(string(sqlBytes))
	if status != "" {
		return status, detail
	}
	if got == string(want) {
		return "pass", ""
	}
	return "fail", firstDiff(string(want), got)
}

// goFormat parses and formats sql, recovering panics. On success it returns
// (output, "", ""); on failure ("", status, detail).
func goFormat(sql string) (out, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			out, status, detail = "", "panic", fmt.Sprintf("%v", r)
		}
	}()
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return "", "parse-error", err.Error()
	}
	return FormatSQL(stmt), "", ""
}

// firstDiff returns a human-readable description of the first byte that differs.
func firstDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("byte %d: want %q, got %q\n--- want ---\n%s\n--- got ---\n%s",
				i, want[i], got[i], want, got)
		}
	}
	return fmt.Sprintf("length: want %d, got %d\n--- want ---\n%s\n--- got ---\n%s",
		len(want), len(got), want, got)
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
	t.Logf("baseline updated: %s", baselinePath)
}

func TestFormatGolden(t *testing.T) {
	ids := collectCases(t)
	results := make(map[string]string, len(ids))
	for _, id := range ids {
		status, detail := runFormatCase(id)
		results[id] = status
		if status == "pass" {
			t.Logf("PASS  %s", id)
		} else {
			t.Logf("%-12s %s — %s", status, id, detail)
		}
	}

	if *acceptFlag {
		writeBaseline(t, results)
		return
	}

	base := readBaseline(t)
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id] // missing => "" => non-pass
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want != "pass" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make format-accept` to update baseline", id)
		}
	}

	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	t.Logf("format scoreboard: %d/%d byte-identical", counts["pass"], len(results))
	if regressions == 0 && improvements == 0 {
		t.Logf("baseline consistent, no regression")
	}
}
