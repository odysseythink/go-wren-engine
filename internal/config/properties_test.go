package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestParsePropertiesBasicKV(t *testing.T) {
	input := "key1=value1\nkey2=value2"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(props))
	}
	if props["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %s", props["key1"])
	}
	if props["key2"] != "value2" {
		t.Errorf("expected key2=value2, got %s", props["key2"])
	}
}

func TestParsePropertiesEmptyValue(t *testing.T) {
	input := "key1="
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(props))
	}
	if v, ok := props["key1"]; !ok || v != "" {
		t.Errorf("expected key1='', got %q", v)
	}
}

func TestParsePropertiesComments(t *testing.T) {
	input := "# comment1\n! comment2\n  # indented comment\nkey=value"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(props))
	}
	if props["key"] != "value" {
		t.Errorf("expected key=value, got %s", props["key"])
	}
}

func TestParsePropertiesColonSeparator(t *testing.T) {
	input := "key:value"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if props["key"] != "value" {
		t.Errorf("expected key=value, got %s", props["key"])
	}
}

func TestParsePropertiesWhitespaceSeparator(t *testing.T) {
	input := "key value"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if props["key"] != "value" {
		t.Errorf("expected key=value, got %s", props["key"])
	}
}

func TestParsePropertiesLineContinuation(t *testing.T) {
	input := "key=hello \\\n  world"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "hello world"
	if props["key"] != expected {
		t.Errorf("expected key=%q, got %q", expected, props["key"])
	}
}

func TestParsePropertiesEscapes(t *testing.T) {
	input := `key=\n\r\t\\=\:\#\!`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "\n\r\t\\=:#!"
	if props["key"] != expected {
		t.Errorf("expected %q, got %q", expected, props["key"])
	}
}

func TestParsePropertiesUnicodeEscape(t *testing.T) {
	input := `key=caf\u00e9`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "café"
	if props["key"] != expected {
		t.Errorf("expected %q, got %q", expected, props["key"])
	}
}

func TestParsePropertiesKeyWithSpaceEscape(t *testing.T) {
	input := `key\ with\ space=value`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if props["key with space"] != "value" {
		t.Errorf("expected 'key with space'=value, got %q", props["key with space"])
	}
}

func TestParsePropertiesMultipleSeparators(t *testing.T) {
	input := "a=1\nb:2\nc 3"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(props))
	}
	if props["a"] != "1" || props["b"] != "2" || props["c"] != "3" {
		t.Errorf("unexpected values: %v", props)
	}
}

func TestParsePropertiesApacheLicenseHeader(t *testing.T) {
	input := `# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.
key=value`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(props))
	}
	if props["key"] != "value" {
		t.Errorf("expected key=value, got %s", props["key"])
	}
}

func TestParsePropertiesEmptyLines(t *testing.T) {
	input := "\n\nkey=value\n\n"
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(props))
	}
	if props["key"] != "value" {
		t.Errorf("expected key=value, got %s", props["key"])
	}
}

func TestParsePropertiesRealWorldExample(t *testing.T) {
	input := `duckdb.connector.init-sql-path=init.sql
duckdb.connector.session-sql-path=session.sql
wren.engine.enabled=true
wren.engine.port=8080`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(props))
	}
	if props["duckdb.connector.init-sql-path"] != "init.sql" {
		t.Errorf("unexpected value for duckdb.connector.init-sql-path: %q", props["duckdb.connector.init-sql-path"])
	}
	if props["duckdb.connector.session-sql-path"] != "session.sql" {
		t.Errorf("unexpected value for duckdb.connector.session-sql-path: %q", props["duckdb.connector.session-sql-path"])
	}
	if props["wren.engine.enabled"] != "true" {
		t.Errorf("unexpected value for wren.engine.enabled: %q", props["wren.engine.enabled"])
	}
	if props["wren.engine.port"] != "8080" {
		t.Errorf("unexpected value for wren.engine.port: %q", props["wren.engine.port"])
	}
}

func TestWritePropertiesRoundTrip(t *testing.T) {
	original := map[string]string{
		"key1":  "value1",
		"key2":  "hello world",
		"key3":  "a=b:c#d!e",
		"key\n": "line1\nline2",
	}
	var buf bytes.Buffer
	err := writePropertiesWithTimestamp(&buf, original, "header", "timestamp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := parseProperties(&buf)
	if err != nil {
		t.Fatalf("unexpected error parsing: %v", err)
	}
	if len(parsed) != len(original) {
		t.Fatalf("expected %d entries, got %d", len(original), len(parsed))
	}
	for k, v := range original {
		if parsed[k] != v {
			t.Errorf("key %q: expected %q, got %q", k, v, parsed[k])
		}
	}
}

func TestParsePropertiesFormFeedEscape(t *testing.T) {
	input := `key=hello\fworld`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "hello\fworld"
	if props["key"] != expected {
		t.Errorf("expected %q, got %q", expected, props["key"])
	}
}

func TestParsePropertiesMalformedUnicode(t *testing.T) {
	input := `key=abc\uXYZ\u`
	props, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "abc\\uXYZ\\u"
	if props["key"] != expected {
		t.Errorf("expected %q, got %q", expected, props["key"])
	}
}

func TestWritePropertiesNonBMPEscape(t *testing.T) {
	original := map[string]string{
		"emoji": "💩",
	}
	var buf bytes.Buffer
	err := writePropertiesWithTimestamp(&buf, original, "header", "timestamp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := parseProperties(&buf)
	if err != nil {
		t.Fatalf("unexpected error parsing: %v", err)
	}
	if parsed["emoji"] != "💩" {
		t.Errorf("expected emoji=💩, got %q", parsed["emoji"])
	}
}

func TestWritePropertiesSortOrder(t *testing.T) {
	props := map[string]string{
		"zebra": "1",
		"apple": "2",
		"mango": "3",
	}
	var buf bytes.Buffer
	err := writePropertiesWithTimestamp(&buf, props, "header", "timestamp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	var dataLines []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "#") {
			dataLines = append(dataLines, line)
		}
	}
	if len(dataLines) != 3 {
		t.Fatalf("expected 3 data lines, got %d", len(dataLines))
	}
	if !strings.HasPrefix(dataLines[0], "apple=") {
		t.Errorf("expected first data line to start with apple=, got %s", dataLines[0])
	}
	if !strings.HasPrefix(dataLines[1], "mango=") {
		t.Errorf("expected second data line to start with mango=, got %s", dataLines[1])
	}
	if !strings.HasPrefix(dataLines[2], "zebra=") {
		t.Errorf("expected third data line to start with zebra=, got %s", dataLines[2])
	}
}
