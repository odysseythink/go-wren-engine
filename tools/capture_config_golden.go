//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
)

type entry struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

func main() {
	resp, err := http.Get("http://localhost:18080/v1/config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var entries []entry
	if err := json.Unmarshal(body, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "unmarshal: %v\n", err)
		os.Exit(1)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	outDir := "testdata/difftest/golden-config"
	_ = os.MkdirAll(outDir, 0o755)

	allBytes, _ := json.Marshal(entries)
	_ = os.WriteFile(outDir+"/all.json", allBytes, 0o644)

	for _, e := range entries {
		b, _ := json.Marshal(e)
		_ = os.WriteFile(fmt.Sprintf("%s/%s.json", outDir, e.Name), b, 0o644)
	}
	fmt.Printf("captured %d config entries (sorted)\n", len(entries))
}
