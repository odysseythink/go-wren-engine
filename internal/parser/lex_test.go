package parser

import "testing"

func TestLexTokens_SkipsWhitespace(t *testing.T) {
	toks := LexTokens("select   a  from t")
	if len(toks) != 4 {
		t.Fatalf("want 4 tokens, got %d: %+v", len(toks), toks)
	}
	wantText := []string{"select", "a", "from", "t"}
	for i, w := range wantText {
		if toks[i].Text != w {
			t.Errorf("token %d: want %q, got %q", i, w, toks[i].Text)
		}
	}
}

func TestLexTokens_SkipsComments(t *testing.T) {
	toks := LexTokens("select a -- a trailing comment\nfrom t")
	if len(toks) != 4 {
		t.Fatalf("want 4 tokens (comment excluded), got %d: %+v", len(toks), toks)
	}
}
