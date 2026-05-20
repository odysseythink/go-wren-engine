// Command capture-duckdb-golden replays every difftest case against a running
// Java wren-engine with modelingOnly=false (post DuckDB conversion) and
// freezes the dialect-converted SQL into golden files under golden-duckdb/.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus directory")
	outDir := flag.String("out", "testdata/difftest/golden-duckdb", "DuckDB golden output dir")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var ok, errs int
	for _, c := range cases {
		body, _ := json.Marshal(map[string]any{
			"manifest":     json.RawMessage(c.ManifestJSON),
			"sql":          c.SQL,
			"modelingOnly": false, // ← always false: capture DUCKDB-dialect output
		})
		req, _ := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("\ncaptured %d golden, %d oracle errors, %d total (golden-duckdb)\n", ok, errs, len(cases))
}
