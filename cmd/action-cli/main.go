package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "action-cli",
		Short: "Test CLI for executing primitive actions against a cluster",
	}

	var (
		kubeconfig     string
		namespace      string
		group          string
		version        string
		resource       string
		name           string
		action         string
		patchBody      string
		clusterID      string
		callerID       string
		clusterVersion string
		allowedNS      string
		allowedSecrets string
		clusterScoped  bool
	)

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a primitive action (get, patch, delete)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logrus.New()
			logger.SetLevel(logrus.DebugLevel)
			logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

			nsList := splitCSV(allowedNS)
			if len(nsList) == 0 && namespace != "" {
				nsList = []string{namespace}
			}
			secList := splitCSV(allowedSecrets)

			auditor := audit.NewMockLogger(logger)
			authz := authorization.New(logger, nsList, secList)
			bp := backplane.NewKubeconfigProvider(logger, kubeconfig)
			exec := executor.New(logger, authz, auditor, bp)

			var act actions.Action
			switch action {
			case "get":
				act = actions.NewGetAction()
			case "patch":
				act = actions.NewPatchAction()
			case "delete":
				act = actions.NewDeleteAction()
			case "describe-nodes":
				act = actions.NewDescribeNodesAction()
				clusterScoped = true
				resource = "nodes"
			default:
				return fmt.Errorf("unknown action %q, must be one of: get, patch, delete, describe-nodes", action)
			}

			if resource == "" {
				return fmt.Errorf("--resource is required for action %q", action)
			}

			params := make(map[string]string)
			if patchBody != "" {
				params["patch"] = patchBody
			}

			result := exec.Execute(context.Background(), executor.Request{
				CallerID:       callerID,
				ClusterID:      clusterID,
				ClusterVersion: clusterVersion,
				Action:         act,
				Target: actions.ResourceTarget{
					Group:         group,
					Version:       version,
					Resource:      resource,
					Namespace:     namespace,
					Name:          name,
					ClusterScoped: clusterScoped,
				},
				Params: params,
			})

			fmt.Println()
			fmt.Printf("Allowed:  %v\n", result.Allowed)
			fmt.Printf("Reason:   %s\n", result.Reason)

			if !result.Allowed {
				return fmt.Errorf("denied: %s", result.Reason)
			}

			if result.Error != nil {
				return fmt.Errorf("action failed: %w", result.Error)
			}

			if result.Output != nil {
				fmt.Printf("Message:  %s\n", result.Output.Message)
				if len(result.Output.Resources) > 0 {
					fmt.Printf("Resources (%d):\n", len(result.Output.Resources))
					for _, r := range result.Output.Resources {
						data, err := json.MarshalIndent(r.Object, "  ", "  ")
						if err != nil {
							fmt.Printf("  (failed to marshal: %v)\n", err)
							continue
						}
						fmt.Printf("  %s\n", data)
					}
				}
			}

			return nil
		},
	}

	runCmd.Flags().StringVar(&kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "path to kubeconfig")
	runCmd.Flags().StringVar(&action, "action", "", "action to execute: get, patch, delete")
	runCmd.Flags().StringVar(&namespace, "namespace", "", "target namespace")
	runCmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	runCmd.Flags().StringVar(&version, "version", "v1", "API version")
	runCmd.Flags().StringVar(&resource, "resource", "", "resource type (plural, e.g. configmaps)")
	runCmd.Flags().StringVar(&name, "name", "", "resource name (empty for list)")
	runCmd.Flags().StringVar(&patchBody, "patch", "", "JSON merge patch body (for patch action)")
	runCmd.Flags().StringVar(&clusterID, "cluster-id", "local", "cluster identifier for audit")
	runCmd.Flags().StringVar(&callerID, "caller-id", "cli-user", "caller identity for audit")
	runCmd.Flags().StringVar(&clusterVersion, "cluster-version", "", "cluster OpenShift version")
	runCmd.Flags().StringVar(&allowedNS, "allowed-namespaces", "", "comma-separated namespace allowlist (defaults to --namespace)")
	runCmd.Flags().StringVar(&allowedSecrets, "allowed-secrets", "", "comma-separated secret allowlist (namespace/name)")
	runCmd.Flags().BoolVar(&clusterScoped, "cluster-scoped", false, "target a cluster-scoped resource (e.g. nodes, namespaces, clusterroles)")

	_ = runCmd.MarkFlagRequired("action")

	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
