// Command capture-analysis-golden replays every difftest case against the
// Java wren-engine /v2/analysis/sql endpoint and freezes normalized JSON.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "java oracle URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus root")
	outDir := flag.String("out", "testdata/difftest/golden-analysis", "output directory")
	groupsFlag := flag.String("groups", "all", "comma-separated groups, or 'all'")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	// Backward compatibility: accept up to 2 positional args
	javaURL := strings.TrimSuffix(*addr, "/")
	if len(flag.Args()) >= 1 {
		javaURL = strings.TrimSuffix(flag.Args()[0], "/")
	}
	if len(flag.Args()) >= 2 {
		*casesDir = flag.Args()[1]
	}
	_ = os.MkdirAll(*outDir, 0o755)

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

		groupDir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(groupDir, 0o755)
		outPath := filepath.Join(groupDir, c.Name+".json")

		reqBody, _ := json.Marshal(map[string]any{
			"manifestStr": base64.StdEncoding.EncodeToString(c.ManifestJSON),
			"sql":         c.SQL,
		})
		req, err := http.NewRequest(http.MethodGet, javaURL+"/v2/analysis/sql", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: build request: %v\n", c.ID(), err)
			errs++
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: request error after %d retries: %v\n", c.ID(), *retryCount, err)
			errs++
			continue
		}
		body := &bytes.Buffer{}
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			_ = os.WriteFile(outPath+".error", body.Bytes(), 0o644)
			_ = os.Remove(outPath)
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
			errs++
			continue
		}

		// Normalize: sort exprSources arrays for reproducible ordering.
		var payload []map[string]any
		if err := json.Unmarshal(body.Bytes(), &payload); err == nil {
			sortExprSourcesInPayload(payload)
			body.Reset()
			b, _ := json.MarshalIndent(payload, "", "  ")
			body.Write(b)
		}

		_ = os.WriteFile(outPath, body.Bytes(), 0o644)
		_ = os.Remove(outPath + ".error")
		fmt.Printf("OK    %s\n", c.ID())
		ok++
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (analysis)\n", ok, errs, total)
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
