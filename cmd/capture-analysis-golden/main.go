package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: capture-analysis-golden <java-url> [corpus-root]")
		os.Exit(1)
	}
	javaURL := strings.TrimSuffix(os.Args[1], "/")
	corpusRoot := "testdata/difftest/cases"
	if len(os.Args) >= 3 {
		corpusRoot = os.Args[2]
	}
	outDir := "testdata/difftest/golden-analysis"
	_ = os.MkdirAll(outDir, 0755)

	cases, err := difftest.LoadCorpus(corpusRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load corpus: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, c := range cases {
		groupDir := filepath.Join(outDir, c.Group)
		_ = os.MkdirAll(groupDir, 0755)
		outPath := filepath.Join(groupDir, c.Name+".json")

		reqBody, _ := json.Marshal(map[string]any{
			"manifestStr": base64.StdEncoding.EncodeToString(c.ManifestJSON),
			"sql":         c.SQL,
		})
		// Java JAX-RS @GET + body; matches cmd/capture-golden's http.MethodGet.
		req, err := http.NewRequest(http.MethodGet, javaURL+"/v2/analysis/sql", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: build request: %v\n", c.ID(), err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: request error: %v\n", c.ID(), err)
			continue
		}
		body := &bytes.Buffer{}
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			_ = os.WriteFile(outPath+".error", body.Bytes(), 0644)
			fmt.Printf("%s: oracle error\n", c.ID())
			continue
		}

		// Normalize: sort exprSources arrays for reproducible ordering (risk #2).
		var payload []map[string]any
		if err := json.Unmarshal(body.Bytes(), &payload); err == nil {
			sortExprSourcesInPayload(payload)
			body.Reset()
			b, _ := json.MarshalIndent(payload, "", "  ")
			body.Write(b)
		}

		_ = os.WriteFile(outPath, body.Bytes(), 0644)
		fmt.Printf("%s: captured\n", c.ID())
	}
}

// sortExprSourcesInPayload recursively sorts every "exprSources" array in the
// analysis JSON so the golden is reproducible across Java runs (risk #2).
func sortExprSourcesInPayload(v any) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			sortExprSourcesInPayload(item)
		}
	case map[string]any:
		if es, ok := x["exprSources"].([]any); ok {
			sort.Slice(es, func(i, j int) bool {
				mi, mj := es[i].(map[string]any), es[j].(map[string]any)
				li, lj := 0.0, 0.0
				ci, cj := 0.0, 0.0
				if nl, ok := mi["nodeLocation"].(map[string]any); ok {
					li, _ = nl["line"].(float64)
					ci, _ = nl["column"].(float64)
				}
				if nl, ok := mj["nodeLocation"].(map[string]any); ok {
					lj, _ = nl["line"].(float64)
					cj, _ = nl["column"].(float64)
				}
				if li != lj {
					return li < lj
				}
				if ci != cj {
					return ci < cj
				}
				si, _ := mi["expression"].(string)
				sj, _ := mj["expression"].(string)
				return si < sj
			})
			x["exprSources"] = es
		}
		for _, val := range x {
			sortExprSourcesInPayload(val)
		}
	}
}
