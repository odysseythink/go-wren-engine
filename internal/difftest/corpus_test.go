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
	var c Case
	for _, cc := range cases {
		if cc.Group == "tpch" {
			c = cc
			break
		}
	}
	if c.Group != "tpch" {
		t.Fatalf("tpch group not found in corpus")
	}
	if !c.ModelingOnly {
		t.Errorf("ModelingOnly = false, want true")
	}
	if len(c.ManifestJSON) == 0 || c.SQL == "" {
		t.Errorf("case %s: empty manifest or sql", c.ID())
	}
}
