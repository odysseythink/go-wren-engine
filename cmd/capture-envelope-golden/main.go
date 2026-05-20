// Command capture-envelope-golden replays every case against a running Java
// wren-engine via /v1/mdl/preview and freezes the JSON envelope under
// testdata/difftest/golden-envelope/.
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
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus dir")
	outDir := flag.String("out", "testdata/difftest/golden-envelope", "envelope golden dir")
	groupsFlag := flag.String("groups", "exec_smoke,viewenum", "comma-separated corpus groups, or 'all'")
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
	ok, errs, total := 0, 0, 0

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

		body, _ := json.Marshal(map[string]any{
			"manifest": json.RawMessage(c.ManifestJSON),
			"sql":      c.SQL,
			"limit":    100,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/preview", bytes.NewReader(body))
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
		base := filepath.Join(dir, c.Name+".json")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			_ = os.Remove(base + ".error.permanent")
			ok++
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			_ = os.Remove(base + ".error.permanent")
			errs++
		}
		fmt.Printf("%s %s\n", map[bool]string{true: "OK   ", false: "ERROR"}[resp.StatusCode/100 == 2], c.ID())
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (envelope, groups=%s)\n", ok, errs, total, *groupsFlag)
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
