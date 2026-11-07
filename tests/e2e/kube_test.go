//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Several actions below pass a namespace they do not act on. The server
// derives a request's target from its params and never marks one
// cluster-scoped (see the comment in internal/executor.KubeScope.DeriveTarget),
// so with a namespace allowlist configured the authorizer refuses any request
// that carries no namespace at all. The namespace must therefore be present
// and allow-listed even for an action that reads cluster-scoped objects; the
// RBAC those actions request is unaffected, and stays cluster-scoped.

func TestGet_ListPods(t *testing.T) {
	output := runAction(t, "get", map[string]string{
		"version":   "v1",
		"resource":  "pods",
		"namespace": env.readNamespace,
	})

	if len(output.Resources) == 0 {
		t.Fatalf("no pods returned from %s", env.readNamespace)
	}
	if name := resourceName(output.Resources[0]); name == "" {
		t.Errorf("first pod has no metadata.name: %v", output.Resources[0])
	}
	if output.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestGet_SinglePod(t *testing.T) {
	name := firstPodName(t)

	output := runAction(t, "get", map[string]string{
		"version":   "v1",
		"resource":  "pods",
		"namespace": env.readNamespace,
		"name":      name,
	})

	if len(output.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(output.Resources))
	}
	if got := resourceName(output.Resources[0]); got != name {
		t.Errorf("got pod %q, want %q", got, name)
	}
}

func TestDescribeNodes_AllNodes(t *testing.T) {
	output := runAction(t, "describe-nodes", map[string]string{
		"namespace": env.readNamespace,
	})

	if len(output.Resources) == 0 {
		t.Fatal("no nodes described")
	}

	// describe-nodes returns a shape of its own rather than Node objects: a
	// flat map per node, correlating the node with its pods, events and lease.
	node := output.Resources[0]
	if name, _ := node["name"].(string); name == "" {
		t.Errorf("first node has no name: %v", node)
	}
	if conditions, ok := node["conditions"].([]interface{}); !ok || len(conditions) == 0 {
		t.Error("expected conditions on the first node")
	}
}

func TestDescribeNodes_SingleNode(t *testing.T) {
	all := runAction(t, "describe-nodes", map[string]string{
		"namespace": env.readNamespace,
	})
	if len(all.Resources) == 0 {
		t.Fatal("no nodes described")
	}
	name, _ := all.Resources[0]["name"].(string)
	if name == "" {
		t.Fatalf("first node has no name: %v", all.Resources[0])
	}

	output := runAction(t, "describe-nodes", map[string]string{
		"namespace": env.readNamespace,
		"name":      name,
	})

	if len(output.Resources) != 1 {
		t.Fatalf("got %d node descriptions, want 1", len(output.Resources))
	}
	if got, _ := output.Resources[0]["name"].(string); got != name {
		t.Errorf("described node %q, want %q", got, name)
	}
}

func TestGetPullSecretEmail(t *testing.T) {
	// Backplane refuses the RBAC this action asks for. Its DDR-90 check
	// (pkg/validation/rbac_validation.go) rejects get on secrets in the core
	// API group for every trusted action, whatever the cluster type, and
	// ignores resourceNames — so narrowing to pull-secret does not help.
	// backplane-api!946 (ROSAENG-66552) scopes that check to infra clusters,
	// which is what DDR-90 intended, and this test passes once it ships.
	//
	// tests/actions asserts today's behaviour from the other side, against
	// the mock: TestGetPullSecretEmail_ForbiddenByBackplane. Both that test
	// and the mock's FORBIDDEN_POLICIES change when this skip comes out.
	t.Skip("blocked on backplane-api!946 (ROSAENG-66552): backplane refuses get on secrets outside infra clusters")

	// The action reads openshift-config/pull-secret and takes no parameters.
	// The namespace is passed only to get past the authorizer, which judges
	// the derived target rather than what the action goes on to read.
	output := runAction(t, "get-pull-secret-email", map[string]string{
		"namespace": env.pullSecretNamespace,
	})

	if len(output.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(output.Resources))
	}
	email, _ := output.Resources[0]["email"].(string)
	if !strings.Contains(email, "@") {
		t.Errorf("got %q, want an email address", email)
	}
}

func TestListAlerts(t *testing.T) {
	// The only action that executes in a pod, so it is the only one covering
	// the backplane pod-exec path end to end.
	output := runAction(t, "list-alerts", map[string]string{
		"namespace": env.readNamespace,
		"state":     "firing",
	})

	if len(output.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(output.Resources))
	}
	// A healthy cluster may legitimately have no firing alerts, so the
	// assertion is on the shape of the answer, not on its contents.
	if _, ok := output.Resources[0]["alerts"].(map[string]interface{}); !ok {
		t.Errorf("expected an alerts object in the output: %v", output.Resources[0])
	}
	if output.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestPatch_ConfigMap(t *testing.T) {
	requireFixtures(t)

	// Unique per run so a patch that silently does nothing cannot pass by
	// matching a value an earlier run left behind.
	value := time.Now().UTC().Format(time.RFC3339Nano)

	output := runAction(t, "patch", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": env.fixtureNamespace,
		"name":      env.fixtureConfigMap,
		"patch":     fmt.Sprintf(`{"data":{"e2e":%q}}`, value),
	})

	if len(output.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(output.Resources))
	}
	if got := resourceName(output.Resources[0]); got != env.fixtureConfigMap {
		t.Errorf("patched %q, want %q", got, env.fixtureConfigMap)
	}

	data, ok := output.Resources[0]["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("patched configmap has no data: %v", output.Resources[0])
	}
	if got, _ := data["e2e"].(string); got != value {
		t.Errorf("data.e2e is %q, want %q", got, value)
	}
}

func TestDelete_Pod(t *testing.T) {
	requireFixtures(t)

	output := runAction(t, "delete", map[string]string{
		"version":   "v1",
		"resource":  "pods",
		"namespace": env.fixtureNamespace,
		"name":      env.fixturePod,
	})

	// delete reports what it did in the message and returns no resources.
	if output.Message == "" {
		t.Error("expected a non-empty message")
	}
	if len(output.Resources) != 0 {
		t.Errorf("got %d resources, want none", len(output.Resources))
	}
}

// firstPodName lists the read namespace and returns a pod name from it, so a
// test that needs an existing object does not depend on one being named in
// the environment.
func firstPodName(t *testing.T) string {
	t.Helper()

	output := runAction(t, "get", map[string]string{
		"version":   "v1",
		"resource":  "pods",
		"namespace": env.readNamespace,
	})
	if len(output.Resources) == 0 {
		t.Fatalf("no pods in %s to target", env.readNamespace)
	}

	name := resourceName(output.Resources[0])
	if name == "" {
		t.Fatalf("first pod has no metadata.name: %v", output.Resources[0])
	}
	return name
}
