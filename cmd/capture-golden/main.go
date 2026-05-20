// Command capture-golden replays every difftest case against a running Java
// wren-engine and freezes the rewritten SQL into golden files.
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
	outDir := flag.String("out", "testdata/difftest/golden", "golden output directory")
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
	var ok, errs int
	var total int

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

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

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			log.Fatalf("%s: request failed after %d retries (is the oracle up?): %v", c.ID(), *retryCount, err)
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
			os.Remove(base + ".error.permanent")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			writeFile(base+".error", payload)
			os.Remove(base)
			os.Remove(base + ".error.permanent")
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d\n", ok, errs, total)
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

func writeFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
