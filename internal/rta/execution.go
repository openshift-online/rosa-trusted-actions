package rta

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (g *commandGroup) executionCommands() *cobra.Command {
	execution := parent("execution", "View and track action executions",
		"List, inspect, and retrieve output from trusted action executions.",
		"  rta execution list\n  rta execution get <id>\n  rta execution output <id>")

	execution.AddCommand(
		g.executionListCommand(),
		g.executionGetCommand(),
		g.executionOutputCommand(),
	)
	return execution
}

func (g *commandGroup) executionListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List action executions",
		Example: "  rta execution list\n  rta execution list --status running\n  rta execution list --action get --cluster 1234 -o json",
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
			for _, f := range []string{"status", "action", "cluster"} {
				if v := fromFlag(cmd, f); v != "" {
					key := f
					if f == "cluster" {
						key = "target"
					}
					query.Set(key, v)
					filters[f] = v
				}
			}

			list, err := client.ListExecutions(ctx, query)
			if err != nil {
				return err
			}

			if done, err := handleJSONOutputAndEmptyLists(cmd, list.Items, len(list.Items), "executions", filters); done {
				return err
			}

			headers := []string{"ID", "ACTION", "CLUSTER", "STATUS", "CREATED"}
			rows := make([][]string, len(list.Items))
			for i, e := range list.Items {
				rows[i] = []string{
					e.Id.String(),
					e.Action,
					e.TargetCluster,
					string(e.Status),
					e.CreatedAt.Format("2006-01-02 15:04:05"),
				}
			}
			return writeTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	cmd.Flags().String("status", "", "Filter by status (pending, running, succeeded, failed, timed_out)")
	cmd.Flags().String("action", "", "Filter by action name")
	cmd.Flags().String("cluster", "", "Filter by cluster ID")
	addOutputFormatFlag(cmd)
	return cmd
}

func (g *commandGroup) executionGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "get <id>",
		Short:   "Show execution details",
		Example: "  rta execution get abc-123\n  rta execution get abc-123 -o json",
		Args:    requiredArgs("execution ID"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			client, err := g.newClient(cmd, true)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			exec, err := client.GetExecution(ctx, args[0])
			if err != nil {
				return err
			}

			if isJSON(cmd) {
				return writeJSON(cmd.OutOrStdout(), exec)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "ID:        %s\n", exec.Id)
			fmt.Fprintf(w, "Action:    %s\n", exec.Action)
			fmt.Fprintf(w, "Cluster:   %s\n", exec.TargetCluster)
			fmt.Fprintf(w, "Status:    %s\n", exec.Status)
			fmt.Fprintf(w, "Created:   %s\n", exec.CreatedAt.Format("2006-01-02 15:04:05"))
			if exec.CompletedAt != nil {
				fmt.Fprintf(w, "Completed: %s\n", exec.CompletedAt.Format("2006-01-02 15:04:05"))
			}
			if exec.DurationSeconds != nil {
				fmt.Fprintf(w, "Duration:  %ds\n", *exec.DurationSeconds)
			}
			if exec.Username != nil {
				fmt.Fprintf(w, "User:      %s\n", *exec.Username)
			}
			if exec.Jira != nil {
				fmt.Fprintf(w, "Jira:      %s\n", *exec.Jira)
			}
			if exec.DryRun != nil && *exec.DryRun {
				fmt.Fprintln(w, "Dry run:   yes")
			}

			return nil
		},
	}
	addOutputFormatFlag(cmd)
	return cmd
}

func (g *commandGroup) executionOutputCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "output <id>",
		Short:   "Show execution output",
		Example: "  rta execution output abc-123\n  rta execution output abc-123 -o json",
		Args:    requiredArgs("execution ID"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			client, err := g.newClient(cmd, true)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			output, err := client.GetExecutionOutput(ctx, args[0])
			if err != nil {
				return err
			}

			if isJSON(cmd) {
				return writeJSON(cmd.OutOrStdout(), output)
			}

			w := cmd.OutOrStdout()
			if output.Message != "" {
				fmt.Fprintln(w, output.Message)
			}
			if len(output.Resources) > 0 {
				fmt.Fprintf(w, "\nResources (%d):\n", len(output.Resources))
				if err := writeJSON(w, output.Resources); err != nil {
					return err
				}
			}

			return nil
		},
	}
	addOutputFormatFlag(cmd)
	return cmd
}
