package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/awsapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "action-cli",
		Short: "Test CLI for executing primitive actions against a cluster",
	}

	var (
		logLevel       string
		logJSON        bool
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
		severity       string
		alertState     string
		silences       bool

		awsRegion      string
		awsEndpointURL string
		awsInfraID     string
		awsID          string
		awsFilters     string
		awsSince       string
	)

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "debug", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&logJSON, "log-json", false, "enable JSON logging")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute an action (get, patch, delete...)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logrus.New()
			level, err := logrus.ParseLevel(logLevel)
			if err != nil {
				level = logrus.DebugLevel
			}
			logger.SetLevel(level)
			if logJSON {
				logger.SetFormatter(&logrus.JSONFormatter{TimestampFormat: time.RFC3339})
			} else {
				logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, TimestampFormat: time.RFC3339})
			}

			act := actions.CreateAction(action)

			if act == nil {
				return fmt.Errorf("unknown action %q, must be one of: %s", action, strings.Join(actions.GetActionsNames(), ", "))
			}

			// AWS write actions are the exception: each one fixes the kind of
			// object it acts on, and AWSScope refuses any --resource that differs
			// from it. Requiring the flag here would oblige the caller to repeat
			// an internal constant whose only accepted value no help text names.
			_, isAWSWrite := act.(actions.AWSWriteAction)
			if resource == "" && !isAWSWrite {
				return fmt.Errorf("--resource is required for action %q", action)
			}

			auditor := audit.NewMockLogger(logger)

			params := make(map[string]string)
			if patchBody != "" {
				params["patch"] = patchBody
			}
			if severity != "" {
				params["severity"] = severity
			}
			if alertState != "" {
				params["state"] = alertState
			}
			if silences {
				params["silences"] = "true"
			}

			// Each action's subtype decides which scope can run it, so the CLI
			// wires up the one that claims this action and nothing else.
			var (
				exec   *executor.Executor
				target actions.Target
			)
			if _, isAWS := act.(actions.AWSAction); isAWS {
				// Neither allowlist is a flag. What may be read, and what may
				// be changed, is server-side policy; a knob here would mean
				// this CLI could exercise more than the server ever permits —
				// so what it tests would stop matching what runs.
				//
				// Both read the same environment variables the server reads,
				// rather than CLI-only switches, so exercising something
				// locally means supplying the input a deployment would. There
				// is no built-in region default to fall back on, for the same
				// reason the server has none, so an unset allowlist here
				// denies every AWS action.
				allowedRegions := splitCSV(os.Getenv("ROSA_TA_AWS_ALLOWED_REGIONS"))
				if len(allowedRegions) == 0 {
					return fmt.Errorf("no AWS region allowlist configured: set ROSA_TA_AWS_ALLOWED_REGIONS, " +
						"the same variable the server reads (e.g. ROSA_TA_AWS_ALLOWED_REGIONS=us-east-1)")
				}
				awsAuthz := authorization.NewAWS(logger, authorization.AWSPolicy{
					AllowedRegions:    allowedRegions,
					AllowedOperations: splitCSV(os.Getenv("ROSA_TA_AWS_ALLOWED_OPERATIONS")),
				})
				awsProvider := awsapi.NewStaticProvider(logger, awsapi.StaticConfig{
					Region:      awsRegion,
					EndpointURL: awsEndpointURL,
					InfraID:     awsInfraID,
				})
				exec = executor.New(logger, auditor, executor.NewAWSScope(awsAuthz, awsProvider))

				// target is left nil so the AWS scope derives it from params,
				// the same way the server does.
				// Only flags the caller actually set are passed on: aws-get
				// rejects a param the resource does not accept, and sending
				// every flag unset would make the CLI fail on resources that
				// take no filters.
				//
				// --aws-region is not among them. It configures the static
				// provider above, which is this CLI's stand-in for the region
				// the server resolves from the cluster; the scope asks the
				// provider for it rather than reading it from a param.
				// Sent only when given: aws-get requires one and fails without
				// it, while the write actions supply their own and accept an
				// explicit --resource only when it matches.
				setIfNotEmpty(params, "resource", resource)
				setIfNotEmpty(params, "id", awsID)
				setIfNotEmpty(params, "filters", awsFilters)
				setIfNotEmpty(params, "since", awsSince)
			} else {
				nsList := splitCSV(allowedNS)
				if len(nsList) == 0 && namespace != "" {
					nsList = []string{namespace}
				}
				authz := authorization.New(logger, nsList, splitCSV(allowedSecrets))
				bp := backplane.NewKubeconfigProvider(logger, kubeconfig)
				exec = executor.New(logger, auditor, executor.NewKubeScope(authz, bp))

				target = actions.ResourceTarget{
					Group:         group,
					Version:       version,
					Resource:      resource,
					Namespace:     namespace,
					Name:          name,
					ClusterScoped: clusterScoped,
				}
			}

			result := exec.Execute(context.Background(), executor.Request{
				CallerID:       callerID,
				ClusterID:      clusterID,
				ClusterVersion: clusterVersion,
				Action:         act,
				Target:         target,
				Params:         params,
			})

			fmt.Println()
			fmt.Printf("Allowed:  %v\n", result.Allowed)
			fmt.Printf("Reason:   %s\n", result.Reason)

			// Error first: a request the executor could not build a target for
			// carries an error and no decision, and calling that "denied"
			// would misreport it. A real denial carries no error.
			if result.Error != nil {
				return fmt.Errorf("action failed: %w", result.Error)
			}

			if !result.Allowed {
				return fmt.Errorf("denied: %s", result.Reason)
			}

			if result.Output != nil {
				fmt.Printf("Message:  %s\n", result.Output.Message)
				if len(result.Output.Resources) > 0 {
					fmt.Printf("Resources (%d):\n", len(result.Output.Resources))
					for _, r := range result.Output.Resources {
						data, err := json.MarshalIndent(r, "  ", "  ")
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
	runCmd.Flags().StringVar(&action, "action", "", "action to execute: get, patch, delete, get-pull-secret-email")
	runCmd.Flags().StringVar(&namespace, "namespace", "", "target namespace")
	runCmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	runCmd.Flags().StringVar(&version, "version", "v1", "API version")
	runCmd.Flags().StringVar(&resource, "resource", "", "resource type: Kubernetes plural (e.g. configmaps) or AWS resource (e.g. ec2-instances); not required by aws-reboot/aws-terminate, which fix their own")
	runCmd.Flags().StringVar(&name, "name", "", "resource name (empty for list)")
	runCmd.Flags().StringVar(&patchBody, "patch", "", "JSON merge patch body (for patch action)")
	runCmd.Flags().StringVar(&clusterID, "cluster-id", "local", "cluster identifier for audit")
	runCmd.Flags().StringVar(&callerID, "caller-id", "cli-user", "caller identity for audit")
	runCmd.Flags().StringVar(&clusterVersion, "cluster-version", "", "cluster OpenShift version")
	runCmd.Flags().StringVar(&allowedNS, "allowed-namespaces", "", "comma-separated namespace allowlist (defaults to --namespace)")
	runCmd.Flags().StringVar(&allowedSecrets, "allowed-secrets", "", "comma-separated secret allowlist (namespace/name)")
	runCmd.Flags().BoolVar(&clusterScoped, "cluster-scoped", false, "target a cluster-scoped resource (e.g. nodes, namespaces, clusterroles)")
	runCmd.Flags().StringVar(&severity, "severity", "", "alert severity filter: critical, warning (list-alerts)")
	runCmd.Flags().StringVar(&alertState, "alert-state", "", "alert state: firing, pending, all (list-alerts)")
	runCmd.Flags().BoolVar(&silences, "silences", false, "include active silences (list-alerts)")
	runCmd.Flags().StringVar(&awsRegion, "aws-region", os.Getenv("AWS_REGION"), "AWS region the static provider serves; stands in for the cluster region the server resolves from OCM (all aws-* actions)")
	runCmd.Flags().StringVar(&awsEndpointURL, "aws-endpoint-url", os.Getenv("ROSA_TA_AWS_ENDPOINT_URL"), "AWS endpoint override, e.g. a local emulator (all aws-* actions)")
	// A flag, unlike the write allowlist, because this is not policy: it is the
	// cluster's identity, which the server reads from OCM and this CLI has no
	// OCM to read. It widens nothing — the ownership check still has to pass,
	// and naming the wrong infra ID makes it fail, not pass.
	runCmd.Flags().StringVar(&awsInfraID, "aws-infra-id", "", "cluster infrastructure ID the target instance must be tagged with; stands in for the one the server resolves from OCM (aws-reboot, aws-terminate)")
	runCmd.Flags().StringVar(&awsID, "aws-id", "", "AWS resource ID; empty lists all (aws-get), required and must be an EC2 instance ID (aws-reboot, aws-terminate)")
	runCmd.Flags().StringVar(&awsFilters, "aws-filters", "", "comma-separated Name=Value API filters (aws-get)")
	runCmd.Flags().StringVar(&awsSince, "aws-since", "", "how far back to look, as a Go duration, e.g. 48h (aws-get, cloudtrail-events)")

	_ = runCmd.MarkFlagRequired("action")

	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// setIfNotEmpty records a flag only when it was given a value, so an unset
// flag reaches the action as an absent param rather than a blank one.
func setIfNotEmpty(params map[string]string, name, value string) {
	if value != "" {
		params[name] = value
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
