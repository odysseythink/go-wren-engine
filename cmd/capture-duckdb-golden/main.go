// Command capture-duckdb-golden replays every difftest case against a running
// Java wren-engine with modelingOnly=false (post DuckDB conversion) and
// freezes the dialect-converted SQL into golden files under golden-duckdb/.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus directory")
	outDir := flag.String("out", "testdata/difftest/golden-duckdb", "DuckDB golden output dir")
	groupsFlag := flag.String("groups", "all", "comma-separated corpus groups, or 'all'")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var wantGroups map[string]bool
	if *groupsFlag != "all" {
		wantGroups = map[string]bool{}
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	client := &http.Client{Timeout: *timeout}
	var ok, errs, total int

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

		body, _ := json.Marshal(map[string]any{
			"manifest":     json.RawMessage(c.ManifestJSON),
			"sql":          c.SQL,
			"modelingOnly": false,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("%s: build request: %v", c.ID(), err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			log.Fatalf("%s: request failed after %d retries: %v", c.ID(), *retryCount, err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			_ = os.Remove(base + ".error.permanent")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			_ = os.Remove(base + ".error.permanent")
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (duckdb-dialect)\n", ok, errs, total)
	if errs > 0 {
		os.Exit(1)
	}
}

func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < retries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return nil, lastErr
}
