//go:build e2e

// Package e2e drives the Trusted Actions HTTP API against a running server and
// a real cluster. It deliberately imports nothing from internal/: the suite is
// a client of the API, so anything it needs to know has to be reachable
// through the API, the same way a caller sees it.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// environment is everything the suite needs from its caller — the Prow step in
// CI, a shell locally. None of it is defaulted beyond the harmless cases: a
// guessed cluster ID is a request against somebody else's cluster.
type environment struct {
	baseURL   string
	token     string
	clusterID string
	jira      string

	// readNamespace exists on any ROSA cluster and is only ever read from. It
	// must appear in the server's allowed_namespaces, or every request
	// targeting it is denied before it reaches the cluster.
	readNamespace string

	// pullSecretNamespace is where the pull secret lives. The action hardcodes
	// the secret it reads, so this value only exists to satisfy the
	// authorizer; see the comment on TestGetPullSecretEmail.
	pullSecretNamespace string

	// Fixtures seeded before the suite runs. The tests that change something
	// use these and nothing else, so a run can never mutate a resource it did
	// not create.
	fixtureNamespace string
	fixtureConfigMap string
	fixturePod       string

	pollInterval  time.Duration
	actionTimeout time.Duration
}

var env environment

func TestMain(m *testing.M) {
	var missing []string
	required := func(name string) string {
		value := os.Getenv(name)
		if value == "" {
			missing = append(missing, name)
		}
		return value
	}

	env = environment{
		baseURL:   required("RTA_BASE_URL"),
		token:     required("RTA_TOKEN"),
		clusterID: required("E2E_CLUSTER_ID"),

		// Recorded on every execution and required by the API. Any valid
		// PROJECT-NUMBER works; the default names the story this suite was
		// written for so a stray staging execution can be traced back.
		jira: envOrDefault("E2E_JIRA", "ROSAENG-59940"),

		readNamespace:       envOrDefault("E2E_READ_NAMESPACE", "openshift-monitoring"),
		pullSecretNamespace: envOrDefault("E2E_PULL_SECRET_NAMESPACE", "openshift-config"),

		fixtureNamespace: os.Getenv("E2E_FIXTURE_NAMESPACE"),
		fixtureConfigMap: os.Getenv("E2E_FIXTURE_CONFIGMAP"),
		fixturePod:       os.Getenv("E2E_FIXTURE_POD"),

		pollInterval:  durationOrDefault("E2E_POLL_INTERVAL", 2*time.Second),
		actionTimeout: durationOrDefault("E2E_ACTION_TIMEOUT", 3*time.Minute),
	}

	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "missing required environment: %v\n", missing)
		fmt.Fprintf(os.Stderr, "  RTA_BASE_URL    base URL of a running trusted-actions server, e.g. http://localhost:8080\n")
		fmt.Fprintf(os.Stderr, "  RTA_TOKEN       bearer token for the calling identity\n")
		fmt.Fprintf(os.Stderr, "  E2E_CLUSTER_ID  OCM cluster ID of the cluster to act on\n")
		os.Exit(1)
	}

	if err := waitForHealth(); err != nil {
		fmt.Fprintf(os.Stderr, "server at %s is not healthy: %v\n", env.baseURL, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// waitForHealth polls /health rather than probing once, so a suite started
// alongside the server does not fail on the startup race. /health needs no
// authentication, so reaching it proves the server is up without proving
// anything about the token.
func waitForHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}

	var lastErr error
	for ctx.Err() == nil {
		lastErr = probeHealth(ctx, client)
		if lastErr == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return lastErr
}

func probeHealth(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /health returned %d", resp.StatusCode)
	}
	return nil
}

// requireFixtures skips a test that needs seeded, writable objects. Skipped
// rather than failed: the read-only half of the suite is useful on its own
// while developing against a cluster by hand, and CI always sets these.
func requireFixtures(t *testing.T) {
	t.Helper()
	if env.fixtureNamespace == "" || env.fixtureConfigMap == "" || env.fixturePod == "" {
		t.Skip("fixtures not seeded: set E2E_FIXTURE_NAMESPACE, E2E_FIXTURE_CONFIGMAP and E2E_FIXTURE_POD")
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ignoring %s=%q: %v\n", name, value, err)
		return fallback
	}
	return parsed
}
