package converter

import (
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite"
)

// pgToDuckDB maps Postgres-style function names to their DuckDB equivalents.
// Lookup uses the LOWERCASE suffix (last segment of QualifiedName). Mirrors
// Java DuckDBMetadata.PG_TO_DUCKDB_FUNCTION_NAME_MAPPINGS.
var pgToDuckDB = map[string]string{
	"generate_array": "generate_series",
}

// RewriteFunction lowercases every FunctionCall name and applies the PG→DuckDB
// mapping when a suffix matches. Mirrors Java RewriteFunction.
type RewriteFunction struct{}

func (RewriteFunction) Apply(root ast.Statement) ast.Statement {
	out := rewrite.RewriteNode(root, rewriteFunctionHook)
	return out.(ast.Statement)
}

func rewriteFunctionHook(n ast.Node) (ast.Node, bool) {
	fc, ok := n.(*ast.FunctionCall)
	if !ok {
		return nil, false
	}

	// Pre-rewrite arguments so nested FunctionCalls are also lowercased.
	// P3a RewriteNode stops descent when hook returns (repl, true), so we
	// must explicitly recurse into arguments before returning.
	newArgs := make([]ast.Expression, len(fc.Arguments))
	for i, a := range fc.Arguments {
		rewritten := rewrite.RewriteNode(a, rewriteFunctionHook)
		newArgs[i] = rewritten.(ast.Expression)
	}

	loweredParts := make([]string, len(fc.Name.Parts))
	loweredOriginal := make([]ast.Identifier, len(fc.Name.OriginalParts))
	for i, p := range fc.Name.Parts {
		loweredParts[i] = strings.ToLower(p)
	}
	for i, p := range fc.Name.OriginalParts {
		loweredOriginal[i] = ast.Identifier{Value: strings.ToLower(p.Value)}
	}
	suffix := loweredParts[len(loweredParts)-1]
	if mapped, ok := pgToDuckDB[suffix]; ok {
		loweredParts = []string{mapped}
		loweredOriginal = []ast.Identifier{{Value: mapped}}
	}
	newName := ast.QualifiedName{Parts: loweredParts, OriginalParts: loweredOriginal}
	repl := *fc
	repl.Name = newName
	repl.Arguments = newArgs
	return &repl, true
}
