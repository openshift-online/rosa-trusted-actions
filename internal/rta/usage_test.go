package rta

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

func TestInvalidUsage_IsUsageError(t *testing.T) {
	cmd := &cobra.Command{Use: "test", Example: "  test foo"}
	err := invalidUsage(cmd, "bad input: %s", "x")
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *usageError, got %T", err)
	}
	if ue.Error() != "bad input: x" {
		t.Errorf("message = %q", ue.Error())
	}
}

func TestPrintUsageIfUsageError_PrintsUsage(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{Use: "test", Example: "  test foo"}
	cmd.SetErr(buf)
	err := invalidUsage(cmd, "something wrong")

	PrintUsageIfUsageError(err)

	out := buf.String()
	if out == "" {
		t.Fatal("expected usage output")
	}
	if !bytes.Contains([]byte(out), []byte("Usage:")) {
		t.Errorf("missing Usage: line: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("Examples:")) {
		t.Errorf("missing Examples: line: %s", out)
	}
}

func TestPrintUsageIfUsageError_NoopForRuntimeError(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{Use: "test"}
	cmd.SetErr(buf)

	PrintUsageIfUsageError(fmt.Errorf("runtime error"))
	PrintUsageIfUsageError(nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for non-usage error, got: %s", buf.String())
	}
}
