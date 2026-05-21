//go:build ignore

// Command probe-rewrite-rules runs each rewrite rule step-by-step for a given
// case and prints the formatted SQL after each rule. Used to verify RC-A
// (jinja / unparsed expressions leaking into generated SQL) is fixed.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/difftest"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/rewrite"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: go run tools/probe-rewrite-rules.go <case-id>\n  e.g. go run tools/probe-rewrite-rules.go tpch/m_orders")
	}
	caseID := os.Args[1]

	cases, err := difftest.LoadCorpus("testdata/difftest/cases")
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var c difftest.Case
	for _, candidate := range cases {
		if candidate.ID() == caseID {
			c = candidate
			break
		}
	}
	if c.ID() == "" {
		log.Fatalf("case %q not found", caseID)
	}

	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		log.Fatalf("unmarshal manifest: %v", err)
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)

	fmt.Printf("=== Case: %s ===\nSQL: %s\n\n", c.ID(), c.SQL)

	statement, err := parser.ParseSQL(c.SQL)
	if err != nil {
		log.Fatalf("initial parse: %v", err)
	}

	bailed := false
	for _, rule := range rewrite.AllRules {
		formatted := formatter.FormatSQL(statement)
		fmt.Printf("--- BEFORE %T ---\n%s\n", rule, formatted)

		if strings.Contains(formatted, "{") || strings.Contains(formatted, "}") {
			fmt.Println(">>> WARNING: contains '{' or '}' <<<")
		}

		reparsed, err := parser.ParseSQL(formatted)
		if err != nil {
			fmt.Printf(">>> RE-PARSE ERROR: %v <<<\n", err)
			break
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf(">>> PANIC during %T.Apply: %v <<<\n", rule, r)
					bailed = true
				}
			}()
			statement, err = rule.Apply(reparsed, ctx, analyzed)
			if err != nil {
				fmt.Printf(">>> APPLY ERROR: %v <<<\n", err)
				bailed = true
				return
			}
			fmt.Printf("--- AFTER %T ---\n%s\n\n", rule, formatter.FormatSQL(statement))
		}()
		if bailed {
			break
		}
	}

	final := formatter.FormatSQL(statement)
	fmt.Printf("=== FINAL ===\n%s\n", final)
	if strings.Contains(final, "{") || strings.Contains(final, "}") {
		fmt.Println(">>> WARNING: final output contains '{' or '}' <<<")
		os.Exit(1)
	}
	if bailed {
		fmt.Println(">>> WARNING: a rule panicked or errored <<<")
		os.Exit(1)
	}
	fmt.Println("OK: no '{' or '}' in output")
}
