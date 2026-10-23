package rta

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (g *commandGroup) auditCommands() *cobra.Command {
	audit := parent("audit", "View audit log entries",
		"Query the trusted actions audit trail.",
		"  rta audit list\n  rta audit list --action get -o json")

	audit.AddCommand(g.auditListCommand())
	return audit
}

func (g *commandGroup) auditListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List audit log entries",
		Example: "  rta audit list\n  rta audit list --action get\n  rta audit list --cluster 1234 -o json",
		Args:    rejectUnknownArgsAndSuggestSimilar,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			client, err := g.newClient(cmd, true)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			query := url.Values{}
			filters := map[string]string{}
			for _, f := range []string{"action", "cluster"} {
				if v := fromFlag(cmd, f); v != "" {
					key := f
					if f == "cluster" {
						key = "target"
					}
					query.Set(key, v)
					filters[f] = v
				}
			}

			list, err := client.ListAuditEntries(ctx, query)
			if err != nil {
				return err
			}

			if done, err := handleJSONOutputAndEmptyLists(cmd, list.Items, len(list.Items), "audit entries", filters); done {
				return err
			}

			headers := []string{"TIMESTAMP", "USER", "METHOD", "PATH", "STATUS", "ACTION"}
			rows := make([][]string, len(list.Items))
			for i, e := range list.Items {
				action := ""
				if e.Action != nil {
					action = *e.Action
				}
				rows[i] = []string{
					e.Timestamp.Format("2006-01-02 15:04:05"),
					e.Username,
					string(e.Method),
					e.Path,
					fmt.Sprintf("%d", e.StatusCode),
					action,
				}
			}
			return writeTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	cmd.Flags().String("action", "", "Filter by action name")
	cmd.Flags().String("cluster", "", "Filter by cluster ID")
	addOutputFormatFlag(cmd)
	return cmd
}
