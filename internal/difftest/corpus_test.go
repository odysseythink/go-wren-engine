package difftest

import "testing"

func TestLoadCorpus_TPCH(t *testing.T) {
	cases, err := LoadCorpus("../../testdata/difftest/cases")
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(cases) != 37 {
		t.Fatalf("want 37 cases, got %d", len(cases))
	}
	c := cases[0]
	if c.Group != "tpch" {
		t.Errorf("Group = %q, want tpch", c.Group)
	}
	if !c.ModelingOnly {
		t.Errorf("ModelingOnly = false, want true")
	}
	if len(c.ManifestJSON) == 0 || c.SQL == "" {
		t.Errorf("case %s: empty manifest or sql", c.ID())
	}
}
