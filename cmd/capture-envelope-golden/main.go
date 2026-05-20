// Command capture-envelope-golden replays every case against a running Java
// wren-engine via /v1/mdl/preview and freezes the JSON envelope under
// testdata/difftest/golden-envelope/.
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
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus dir")
	outDir := flag.String("out", "testdata/difftest/golden-envelope", "envelope golden dir")
	groupsFlag := flag.String("groups", "exec_smoke,viewenum", "comma-separated corpus groups to capture")
	flag.Parse()

	wantGroups := map[string]bool{}
	for _, g := range bytes.Split([]byte(*groupsFlag), []byte(",")) {
		wantGroups[string(bytes.TrimSpace(g))] = true
	}

	cases, _ := difftest.LoadCorpus(*casesDir)
	client := &http.Client{Timeout: 60 * time.Second}
	ok, errs := 0, 0
	for _, c := range cases {
		if !wantGroups[c.Group] {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"manifest": json.RawMessage(c.ManifestJSON),
			"sql":      c.SQL,
			"limit":    100,
		})
		req, _ := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/preview", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".json")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			ok++
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			errs++
		}
		fmt.Printf("%s %s\n", map[bool]string{true: "OK   ", false: "ERROR"}[resp.StatusCode/100 == 2], c.ID())
	}
	fmt.Printf("\ncaptured %d envelope, %d errors (groups=%s)\n", ok, errs, *groupsFlag)
}
