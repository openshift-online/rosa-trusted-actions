package rta

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func (g *commandGroup) actionCommands() *cobra.Command {
	action := parent("action", "List, describe, and run trusted actions",
		"Discover available trusted actions and execute them against ROSA clusters.",
		"  rta action list\n  rta action describe get\n  rta action run get --cluster 1234 --jira OHSS-1234")

	action.AddCommand(
		g.actionListCommand(),
		g.actionDescribeCommand(),
		g.actionRunCommand(),
	)
	return action
}

func (g *commandGroup) actionListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List available trusted actions",
		Example: "  rta action list\n  rta action list -o json",
		Args:    rejectUnknownArgsAndSuggestSimilar,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			client, err := g.newClient(cmd, false)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			catalog, err := client.Catalog(ctx)
			if err != nil {
				return err
			}

			if done, err := handleJSONOutputAndEmptyLists(cmd, catalog.Items, len(catalog.Items), "actions", nil); done {
				return err
			}

			headers := []string{"NAME", "SCOPE", "TYPE", "DESCRIPTION"}
			rows := make([][]string, len(catalog.Items))
			for i, a := range catalog.Items {
				rows[i] = []string{a.Name, string(a.Scope), string(a.Type), a.Description}
			}
			return writeTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	addOutputFormatFlag(cmd)
	return cmd
}

func (g *commandGroup) actionDescribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <action>",
		Short:   "Describe a trusted action and its parameters",
		Example: "  rta action describe get\n  rta action describe get -o json",
		Args:    requiredArgs("action name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			client, err := g.newClient(cmd, false)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			action, err := client.Describe(ctx, args[0])
			if err != nil {
				return err
			}

			if isJSON(cmd) {
				return writeJSON(cmd.OutOrStdout(), action)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Name:        %s\n", action.Name)
			fmt.Fprintf(w, "Scope:       %s\n", action.Scope)
			fmt.Fprintf(w, "Type:        %s\n", action.Type)
			fmt.Fprintf(w, "Description: %s\n", action.Description)

			if action.Params != nil && len(*action.Params) > 0 {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "Parameters:")
				headers := []string{"  NAME", "REQUIRED", "DEFAULT", "DESCRIPTION"}
				rows := make([][]string, len(*action.Params))
				for i, p := range *action.Params {
					required := "no"
					if p.Required != nil && *p.Required {
						required = "yes"
					}
					def := ""
					if p.Default != nil {
						def = *p.Default
					}
					desc := ""
					if p.Description != nil {
						desc = *p.Description
					}
					rows[i] = []string{"  " + p.Name, required, def, desc}
				}
				if err := writeTable(w, headers, rows); err != nil {
					return err
				}
			}

			return nil
		},
	}
	addOutputFormatFlag(cmd)
	return cmd
}

func (g *commandGroup) actionRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <action>",
		Short: "Execute a trusted action on a cluster",
		Long:  "Submit an action for execution. By default, waits for completion and displays the output.",
		Example: `  rta action run get --cluster 1234 --jira OHSS-1234 --param resource=pods --param namespace=openshift-monitoring
  rta action run get --cluster 1234 --jira OHSS-1234 --param group=apps --param version=v1 --param resource=deployments --param namespace=openshift-monitoring
  rta action run describe-nodes --cluster 1234 --jira OHSS-1234
  rta action run get --cluster 1234 --jira OHSS-1234 --param resource=pods --no-wait

Parameters are action-specific. Use 'rta action describe <action>' to see
available parameters, their defaults, and which ones are required.`,
		Args: requiredArgs("action name"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}

			params, err := parseParams(cmd)
			if err != nil {
				return err
			}

			client, err := g.newClient(cmd, true)
			if err != nil {
				return err
			}

			ctx, cancel := g.requestContext(cmd)
			defer cancel()

			dryRun := boolFlag(cmd, "dry-run")
			force := boolFlag(cmd, "force")
			req := openapi.ExecutionRequest{
				TargetCluster: fromFlag(cmd, "cluster"),
				Jira:          fromFlag(cmd, "jira"),
				Params:        params,
				DryRun:        &dryRun,
				Force:         &force,
			}

			exec, err := client.CreateExecution(ctx, args[0], req)
			if err != nil {
				return err
			}

			if boolFlag(cmd, "no-wait") {
				if isJSON(cmd) {
					return writeJSON(cmd.OutOrStdout(), exec)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Execution %s submitted (status: %s)\n", exec.Id, exec.Status)
				return nil
			}

			exec, err = g.waitForCompletion(ctx, cmd, client, exec)
			if err != nil {
				return err
			}

			if isJSON(cmd) {
				output, outErr := client.GetExecutionOutput(ctx, exec.Id.String())
				result := struct {
					Execution openapi.Execution        `json:"execution"`
					Output    *openapi.ExecutionOutput `json:"output,omitempty"`
					Error     string                   `json:"error,omitempty"`
				}{Execution: *exec, Output: output}
				if outErr != nil {
					result.Error = fmt.Sprintf("fetch output: %s", outErr)
				}
				if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
				if exec.Status == "failed" || exec.Status == "timed_out" {
					return fmt.Errorf("execution %s: %s", exec.Id, exec.Status)
				}
				return nil
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Execution:  %s\n", exec.Id)
			fmt.Fprintf(w, "Action:     %s\n", exec.Action)
			fmt.Fprintf(w, "Cluster:    %s\n", exec.TargetCluster)
			fmt.Fprintf(w, "Status:     %s\n", exec.Status)
			if exec.DurationSeconds != nil {
				fmt.Fprintf(w, "Duration:   %ds\n", *exec.DurationSeconds)
			}
			if dryRun {
				fmt.Fprintln(w, "\nDry run: no changes were made.")
			}

			if exec.Status == "succeeded" {
				output, err := client.GetExecutionOutput(ctx, exec.Id.String())
				if err != nil {
					return fmt.Errorf("fetch output: %w", err)
				}
				if output.Message != "" {
					fmt.Fprintf(w, "\n%s\n", output.Message)
				}
				if len(output.Resources) > 0 {
					fmt.Fprintf(w, "\nResources (%d):\n", len(output.Resources))
					if err := writeJSON(w, output.Resources); err != nil {
						return err
					}
				}
			}

			if exec.Status == "failed" || exec.Status == "timed_out" {
				if output, err := client.GetExecutionOutput(ctx, exec.Id.String()); err == nil && output.Message != "" {
					return fmt.Errorf("execution %s: %s: %s", exec.Id, exec.Status, output.Message)
				}
				return fmt.Errorf("execution %s: %s", exec.Id, exec.Status)
			}

			return nil
		},
	}
	cmd.Flags().String("cluster", "", "Target cluster ID (required)")
	cmd.Flags().String("jira", "", "Associated Jira ticket, e.g. OHSS-1234 (required)")
	cmd.Flags().StringSlice("param", nil, "Action parameter as key=value (repeatable)")
	cmd.Flags().Bool("force", false, "Override safety preconditions")
	cmd.Flags().Bool("dry-run", false, "Preview the action without executing")
	cmd.Flags().Bool("no-wait", false, "Return immediately after submission")
	_ = cmd.MarkFlagRequired("cluster")
	_ = cmd.MarkFlagRequired("jira")
	addOutputFormatFlag(cmd)
	return cmd
}

func boolFlag(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func parseParams(cmd *cobra.Command) (*map[string]string, error) {
	raw, _ := cmd.Flags().GetStringSlice("param")
	if len(raw) == 0 {
		return nil, nil
	}
	params := make(map[string]string, len(raw))
	for _, kv := range raw {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, invalidUsage(cmd, "invalid --param %q: expected key=value", kv)
		}
		params[parts[0]] = parts[1]
	}
	return &params, nil
}

func (g *commandGroup) requestContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	timeout := g.timeout("timeout")
	return context.WithTimeout(cmd.Context(), timeout)
}

func (g *commandGroup) waitForCompletion(ctx context.Context, cmd *cobra.Command, client *Client, exec *openapi.Execution) (*openapi.Execution, error) {
	terminal := map[openapi.ExecutionStatus]bool{
		"succeeded": true,
		"failed":    true,
		"timed_out": true,
	}
	if terminal[exec.Status] {
		return exec, nil
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Waiting for execution %s...\n", exec.Id)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return exec, fmt.Errorf("timed out waiting for execution %s (last status: %s)", exec.Id, exec.Status)
		case <-ticker.C:
			updated, err := client.GetExecution(ctx, exec.Id.String())
			if err != nil {
				return exec, fmt.Errorf("poll execution: %w", err)
			}
			exec = updated
			if terminal[exec.Status] {
				return exec, nil
			}
		}
	}
}
