// Command capture-golden replays every difftest case against a running Java
// wren-engine and freezes the rewritten SQL into golden files.
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
	outDir := flag.String("out", "testdata/difftest/golden", "golden output directory")
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
			"modelingOnly": c.ModelingOnly,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("%s: build request: %v", c.ID(), err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: request failed (is the oracle up?): %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("%s: mkdir: %v", c.ID(), err)
		}
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			writeFile(base, payload)
			os.Remove(base + ".error")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			writeFile(base+".error", payload)
			os.Remove(base)
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("\ncaptured %d golden, %d oracle errors, %d total\n", ok, errs, len(cases))
}

func writeFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
