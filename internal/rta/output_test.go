package rta

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	err := writeJSON(&buf, map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"key": "value"`) {
		t.Errorf("expected pretty JSON, got: %s", out)
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"NAME", "VALUE"}
	rows := [][]string{{"foo", "bar"}, {"longer-name", "baz"}}
	if err := writeTable(&buf, headers, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Errorf("expected header, got: %s", lines[0])
	}
}

func TestWriteEmptyList_NoFilters(t *testing.T) {
	var buf bytes.Buffer
	if err := writeEmptyList(&buf, "pods", nil); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "No pods found.\n" {
		t.Errorf("got %q", got)
	}
}

func TestWriteEmptyList_WithFilters(t *testing.T) {
	var buf bytes.Buffer
	if err := writeEmptyList(&buf, "executions", map[string]string{"status": "running"}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "No executions found") {
		t.Errorf("missing prefix: %s", got)
	}
	if !strings.Contains(got, "status running") {
		t.Errorf("missing filter: %s", got)
	}
}

func TestWriteEmptyJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writeEmptyJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("got %q", got)
	}
}
