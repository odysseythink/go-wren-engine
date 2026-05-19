package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestRewriteNodeIdentity(t *testing.T) {
	cases := []string{
		"SELECT 1",
		"SELECT a, b FROM t WHERE c > 5 AND d < 10",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT count(*) FROM (SELECT k FROM t GROUP BY k) s",
		"SELECT a FROM t1 JOIN t2 ON t1.id = t2.id ORDER BY a DESC",
		"SELECT CASE WHEN a THEN 1 ELSE 2 END FROM t",
	}
	identity := func(n ast.Node) (ast.Node, bool) { return nil, false }
	for _, sql := range cases {
		stmt, err := parser.ParseSQL(sql)
		if err != nil {
			t.Fatalf("parse %q: %v", sql, err)
		}
		want := formatter.FormatSQL(stmt)
		got := formatter.FormatSQL(RewriteNode(stmt, identity).(ast.Statement))
		if got != want {
			t.Errorf("identity rewrite changed %q:\n want %q\n got  %q", sql, want, got)
		}
	}
}
