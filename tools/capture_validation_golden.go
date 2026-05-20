//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	oracleURL := "http://localhost:18080"
	outDir := "testdata/difftest/golden-validation"
	_ = os.MkdirAll(outDir, 0o755)

	cases := []struct {
		name    string
		reqBody string
	}{
		{"pass", `{"manifest":{"catalog":"test","schema":"test","models":[{"name":"orders","refSql":"SELECT 1 AS orderkey","columns":[{"name":"orderkey","expression":"orderkey","type":"INTEGER"}]}]},"parameters":{"modelName":"orders","columnName":"orderkey"}}`},
		{"fail", `{"manifest":{"catalog":"test","schema":"test","models":[{"name":"orders","refSql":"SELECT 1 AS orderkey","columns":[{"name":"badcol","expression":"badcol","type":"INTEGER"}]}]},"parameters":{"modelName":"orders","columnName":"badcol"}}`},
		{"error_missing_model", `{"manifest":{"catalog":"test","schema":"test","models":[]},"parameters":{"modelName":"","columnName":"x"}}`},
		{"error_missing_column", `{"manifest":{"catalog":"test","schema":"test","models":[{"name":"orders","refSql":"SELECT 1","columns":[]}]},"parameters":{"modelName":"orders","columnName":""}}`},
	}

	for _, c := range cases {
		resp, err := http.Post(oracleURL+"/v1/mdl/validate/column_is_valid", "application/json", bytes.NewReader([]byte(c.reqBody)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch %s: %v\n", c.name, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = os.WriteFile(fmt.Sprintf("%s/%s.json", outDir, c.name), body, 0o644)
		fmt.Printf("captured %s (%d bytes)\n", c.name, len(body))
	}
}
