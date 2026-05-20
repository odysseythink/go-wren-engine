package difftest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

var (
	acceptEnvelopeFlag = flag.Bool("difftest.accept-envelope", false, "rewrite baseline-envelope.json")

	envelopeGroups       = map[string]bool{"exec_smoke": true, "viewenum": true}
	envelopeDir          = "../../testdata/difftest/golden-envelope"
	baselineEnvelopePath = "../../testdata/difftest/baseline-envelope.json"
)

// runEnvelopeCase executes a case against the Go preview pipeline and compares
// the JSON envelope to the Java golden. "Structural equivalence" means: same
// column count, names, types; same row count; cell values compared with
// numeric coercion (Go int32 vs Java Integer both Unmarshal to float64).
func runEnvelopeCase(t *testing.T, c Case) (status, detail string) {
	t.Helper()
	goldenBase := filepath.Join(envelopeDir, c.Group, c.Name+".json")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java preview errored permanently"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java preview errored"
	}
	want, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "envelope golden missing"
	}

	var manifest dto.Manifest
	_ = json.Unmarshal(c.ManifestJSON, &manifest)
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	md := duckdb.NewMetadata()
	defer md.Close()

	// IMPORTANT: for cases/viewenum we need to seed in-memory tables via init SQL.
	// This requires a shared init.sql file per group — task 12 wires this.
	if c.Group == "viewenum" {
		initSQL, err := os.ReadFile("../../testdata/difftest/cases/viewenum/init.sql")
		if err == nil {
			_ = md.SetInitSQL(context.Background(), string(initSQL))
		}
	}

	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	got, err := svc.Preview(context.Background(), wrenMDL, c.SQL, 100)
	if err != nil {
		return "go-error", err.Error()
	}

	var wantEnv, gotEnv dto.PreviewResponse
	if err := json.Unmarshal(want, &wantEnv); err != nil {
		return "parser-gap", "unmarshal Java golden: " + err.Error()
	}
	gotJSON, _ := json.Marshal(got)
	_ = json.Unmarshal(gotJSON, &gotEnv) // round-trip to normalize int→float

	if !envelopeEqual(&wantEnv, &gotEnv) {
		return "fail", envelopeDiff(&wantEnv, &gotEnv)
	}
	return "pass", ""
}

func envelopeEqual(a, b *dto.PreviewResponse) bool {
	if len(a.Columns) != len(b.Columns) || len(a.Data) != len(b.Data) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i].Name != b.Columns[i].Name || a.Columns[i].Type != b.Columns[i].Type {
			return false
		}
	}
	for r := range a.Data {
		if !reflect.DeepEqual(a.Data[r], b.Data[r]) {
			return false
		}
	}
	return true
}

func envelopeDiff(a, b *dto.PreviewResponse) string {
	if len(a.Columns) != len(b.Columns) {
		return "column count differs"
	}
	if len(a.Data) != len(b.Data) {
		return "row count differs"
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return "column " + a.Columns[i].Name + " differs"
		}
	}
	for r := range a.Data {
		if !reflect.DeepEqual(a.Data[r], b.Data[r]) {
			return fmt.Sprintf("row %d differs", r)
		}
	}
	return "unknown"
}

func TestEnvelopeDifferential(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results := map[string]string{}
	for _, c := range cases {
		if !envelopeGroups[c.Group] {
			continue
		}
		s, d := runEnvelopeCase(t, c)
		results[c.ID()] = s
		t.Logf("%-12s %s — %s", s, c.ID(), d)
	}

	if *acceptEnvelopeFlag {
		writeEnvelopeBaseline(t, results)
		return
	}

	base := readEnvelopeBaseline(t)
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			t.Errorf("ENVELOPE REGRESSION %s: baseline=pass, now=%s", id, st)
		case w == "oracle-error-permanent" && st == "pass":
			t.Errorf("ENVELOPE %s now passes (was oracle-error-permanent) — run `make envelope-difftest-accept`", id)
		case w != "pass" && w != "oracle-error-permanent" && st == "pass":
			t.Errorf("ENVELOPE IMPROVEMENT %s now passes — run `make envelope-difftest-accept`", id)
		}
	}
	t.Logf("\n%s", envelopeSummary(results))
}

func readEnvelopeBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselineEnvelopePath)
	if err != nil {
		t.Fatalf("read envelope baseline: %v", err)
	}
	var b baselineFile
	_ = json.Unmarshal(raw, &b)
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func envelopeSummary(results map[string]string) string {
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
	s := fmt.Sprintf("Envelope 差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}

func writeEnvelopeBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, _ := json.MarshalIndent(b, "", "  ")
	_ = os.WriteFile(baselineEnvelopePath, append(raw, '\n'), 0o644)
	t.Logf("baseline-envelope 已更新")
}
