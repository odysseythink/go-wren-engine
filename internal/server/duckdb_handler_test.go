package server_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/server"
)

func TestDuckDBQuery(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })
	h := server.NewDuckDBHandler(md)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/data-source/duckdb/query", "text/plain",
		strings.NewReader("SELECT 1 AS a"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"name":"a"`)) {
		t.Errorf("expected column a, got %s", body)
	}
}

func TestDuckDBInitSQLRoundtrip(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })
	h := server.NewDuckDBHandler(md)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// PUT init-sql
	put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/data-source/duckdb/settings/init-sql",
		strings.NewReader("CREATE TABLE x(a INTEGER); INSERT INTO x VALUES (7);"))
	r2, _ := http.DefaultClient.Do(put)
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("PUT status: %d", r2.StatusCode)
	}

	// GET init-sql
	get, _ := http.Get(srv.URL + "/v1/data-source/duckdb/settings/init-sql")
	body, _ := io.ReadAll(get.Body)
	get.Body.Close()
	if !bytes.Contains(body, []byte("CREATE TABLE x")) {
		t.Errorf("init SQL not echoed, got %q", body)
	}

	// Query against the table created by init.
	q, _ := http.Post(srv.URL+"/v1/data-source/duckdb/query", "text/plain", strings.NewReader("SELECT a FROM x"))
	qbody, _ := io.ReadAll(q.Body)
	q.Body.Close()
	if !bytes.Contains(qbody, []byte("7")) {
		t.Errorf("expected query result with 7, got %s", qbody)
	}
}
