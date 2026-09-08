package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func main() {
	var (
		server       string
		action       string
		clusterID    string
		jira         string
		params       []string
		dryRun       bool
		force        bool
		token        string
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "test-client",
		Short: "HTTP client for testing the Trusted Actions API",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				out, err := exec.Command("ocm", "token").Output()
				if err != nil {
					return fmt.Errorf("--token not set and 'ocm token' failed: %w", err)
				}
				token = strings.TrimSpace(string(out))
			}

			paramMap, err := parseParams(params)
			if err != nil {
				return err
			}

			reqBody := openapi.ExecutionRequest{
				TargetCluster: clusterID,
				Jira:          jira,
			}
			if len(paramMap) > 0 {
				reqBody.Params = &paramMap
			}
			if dryRun {
				reqBody.DryRun = &dryRun
			}
			if force {
				reqBody.Force = &force
			}

			ctx := cmd.Context()
			url := fmt.Sprintf("%s/api/v0/trusted-actions/%s/run", server, action)
			execution, err := doPost(ctx, url, token, reqBody)
			if err != nil {
				return err
			}

			fmt.Printf("Execution %s created (status: %s)\n", execution.Id, execution.Status)

			for !isTerminal(execution.Status) {
				time.Sleep(pollInterval)
				fmt.Printf("  polling... (status: %s)\n", execution.Status)

				pollURL := fmt.Sprintf("%s/api/v0/trusted-actions/runs/%s?include=output", server, execution.Id)
				execution, err = doGet(ctx, pollURL, token)
				if err != nil {
					return fmt.Errorf("poll failed: %w", err)
				}
			}

			fmt.Println()
			out, _ := json.MarshalIndent(execution, "", "  ")
			fmt.Println(string(out))

			if execution.Status != openapi.ExecutionStatusSucceeded {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "http://localhost:8080", "server URL")
	cmd.Flags().StringVar(&action, "action", "", "action name")
	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "target cluster ID")
	cmd.Flags().StringVar(&jira, "jira", "", "Jira ticket (e.g. ROSAENG-1234)")
	cmd.Flags().StringArrayVar(&params, "param", nil, "action parameter as key=value (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "execute dry-run variant")
	cmd.Flags().BoolVar(&force, "force", false, "bypass cooldown/limits")
	cmd.Flags().StringVar(&token, "token", "", "bearer token (default: output of 'ocm token')")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "polling interval")

	_ = cmd.MarkFlagRequired("action")
	_ = cmd.MarkFlagRequired("cluster-id")
	_ = cmd.MarkFlagRequired("jira")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func parseParams(raw []string) (map[string]string, error) {
	m := make(map[string]string, len(raw))
	for _, p := range raw {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --param %q: expected key=value", p)
		}
		m[k] = v
	}
	return m, nil
}

func isTerminal(status openapi.ExecutionStatus) bool {
	return status == openapi.ExecutionStatusSucceeded ||
		status == openapi.ExecutionStatusFailed ||
		status == openapi.ExecutionStatusTimedOut
}

func doPost(ctx context.Context, url, token string, body interface{}) (*openapi.Execution, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	var result openapi.Execution
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func doGet(ctx context.Context, url, token string) (*openapi.Execution, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	var result openapi.Execution
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
