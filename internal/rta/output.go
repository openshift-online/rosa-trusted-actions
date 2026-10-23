package rta

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func isJSON(cmd *cobra.Command) bool {
	return fromFlag(cmd, "output") == "json"
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func writeEmptyList(w io.Writer, resource string, filters map[string]string) error {
	msg := "No " + resource + " found"
	parts := make([]string, 0, len(filters))
	for k, v := range filters {
		if v != "" {
			parts = append(parts, k+" "+v)
		}
	}
	if len(parts) > 0 {
		msg += " matching " + strings.Join(parts, ", ")
	}
	_, err := fmt.Fprintln(w, msg+".")
	return err
}

func writeEmptyJSON(w io.Writer) error {
	_, err := fmt.Fprintln(w, "[]")
	return err
}

// handleJSONOutputAndEmptyLists handles JSON output and empty results for list commands.
func handleJSONOutputAndEmptyLists(cmd *cobra.Command, items any, count int, resource string, filters map[string]string) (bool, error) {
	if isJSON(cmd) {
		if count == 0 {
			return true, writeEmptyJSON(cmd.OutOrStdout())
		}
		return true, writeJSON(cmd.OutOrStdout(), items)
	}
	if count == 0 {
		return true, writeEmptyList(cmd.OutOrStdout(), resource, filters)
	}
	return false, nil
}
