package difftest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wren-engine/wren/internal/config"
)

const (
	goldenConfigDir    = "../../testdata/difftest/golden-config"
	baselineConfigPath = "../../testdata/difftest/baseline-config.json"
)

func TestConfig(t *testing.T) {
	cm := config.NewConfigManager()
	results := map[string]string{}

	// 1. all entries
	all := cm.All()
	gotAll, _ := json.Marshal(all)
	wantAll, err := os.ReadFile(filepath.Join(goldenConfigDir, "all.json"))
	if err != nil {
		results["config/all"] = "no-golden"
	} else if err := JSONEqualWithOptions(wantAll, gotAll, CompareOptions{}); err != nil {
		results["config/all"] = "fail"
		t.Logf("config/all mismatch: %v", err)
	} else {
		results["config/all"] = "pass"
	}

	// 2. each individual entry
	for _, e := range all {
		id := "config/" + e.Name
		want, err := os.ReadFile(filepath.Join(goldenConfigDir, e.Name+".json"))
		if err != nil {
			results[id] = "no-golden"
			continue
		}
		got, _ := json.Marshal(e)
		if err := JSONEqualWithOptions(want, got, CompareOptions{}); err != nil {
			results[id] = "fail"
			t.Logf("%s mismatch: %v", id, err)
		} else {
			results[id] = "pass"
		}
	}

	baseline := loadBaseline(baselineConfigPath)
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
