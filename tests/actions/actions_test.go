//go:build integration

package actiontest

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
)

// ---------------------------------------------------------------------------
// get action
// ---------------------------------------------------------------------------

func TestGetAction_ListPods(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(result.Resources))
	}

	names := map[string]bool{}
	for _, r := range result.Resources {
		names[r.GetName()] = true
	}
	if !names["pod-1"] || !names["pod-2"] {
		t.Errorf("expected pod-1 and pod-2, got %v", names)
	}
}

func TestGetAction_GetSinglePod(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].GetName() != "pod-1" {
		t.Errorf("expected name pod-1, got %s", result.Resources[0].GetName())
	}
}

func TestGetAction_NotFound(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "gone",
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error for non-existent pod, got nil")
	}
}

func TestGetAction_Forbidden(t *testing.T) {
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}, ResourceNames: []string{"other-pod"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error for forbidden access, got nil")
	}
}

func TestGetAction_CrossClusterIsolation(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")
	clusterC2 := "00000000-0000-0000-0000-000000000002"
	client := createMockSession(t, clusterC2, action.RequiredRBAC(target))

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error accessing C1 fixture from C2 session")
	}
}

func TestGetAction_ResponseContentValidation(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	obj := result.Resources[0]
	if obj.GetAPIVersion() != "v1" {
		t.Errorf("expected apiVersion v1, got %s", obj.GetAPIVersion())
	}
	if obj.GetKind() != "Pod" {
		t.Errorf("expected kind Pod, got %s", obj.GetKind())
	}
	if obj.GetNamespace() != "ns-a" {
		t.Errorf("expected namespace ns-a, got %s", obj.GetNamespace())
	}
	labels := obj.GetLabels()
	if labels["app"] != "demo" {
		t.Errorf("expected label app=demo, got %v", labels)
	}
	status, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if status != "Running" {
		t.Errorf("expected status.phase Running, got %s", status)
	}
}

func TestGetAction_EmptyList(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "default",
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 configmaps, got %d", len(result.Resources))
	}
}

func TestGetAction_ClusterScoped_ListNamespaces(t *testing.T) {
	target := actions.ResourceTarget{
		Version:       "v1",
		Resource:      "namespaces",
		ClusterScoped: true,
	}
	action := actions.CreateAction("get")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 namespaces, got %d", len(result.Resources))
	}
	names := map[string]bool{}
	for _, r := range result.Resources {
		names[r.GetName()] = true
	}
	if !names["default"] || !names["ns-a"] {
		t.Errorf("expected default and ns-a, got %v", names)
	}
}

func TestGetAction_RBACVerbMismatch(t *testing.T) {
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error when session only grants list but get is needed")
	}
}

func TestGetAction_WildcardRBAC(t *testing.T) {
	rbac := []backplane.RBACRule{
		{APIGroups: []string{"*"}, Resources: []string{"pods"}, Verbs: []string{"*"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("get")

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Resources[0].GetName() != "pod-1" {
		t.Errorf("expected pod-1, got %s", result.Resources[0].GetName())
	}
}

// ---------------------------------------------------------------------------
// patch action
// ---------------------------------------------------------------------------

func TestPatchAction_HappyPath(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "default",
		Name:      "test-proxy-mutation",
	}
	action := actions.CreateAction("patch")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	params := map[string]string{"patch": `{"data":{"key1":"updated"}}`}
	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{
		Target: target,
		Params: params,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].GetName() != "test-proxy-mutation" {
		t.Errorf("expected name test-proxy-mutation, got %s", result.Resources[0].GetName())
	}
}

func TestPatchAction_MissingName(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "default",
	}
	action := actions.CreateAction("patch")
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"patch"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	params := map[string]string{"patch": `{"data":{"key1":"updated"}}`}
	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{
		Target: target,
		Params: params,
	})
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestPatchAction_MissingPatchParam(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "default",
		Name:      "test-proxy-mutation",
	}
	action := actions.CreateAction("patch")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{
		Target: target,
	})
	if err == nil {
		t.Fatal("expected error for missing patch param, got nil")
	}
}

func TestPatchAction_ForbiddenByRBAC(t *testing.T) {
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "configmaps",
		Namespace: "default",
		Name:      "test-proxy-mutation",
	}
	action := actions.CreateAction("patch")
	params := map[string]string{"patch": `{"data":{"key1":"updated"}}`}

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{
		Target: target,
		Params: params,
	})
	if err == nil {
		t.Fatal("expected error when session only grants get but patch is needed")
	}
}

// ---------------------------------------------------------------------------
// delete action
// ---------------------------------------------------------------------------

func TestDeleteAction_HappyPath(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("delete")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestDeleteAction_MissingName(t *testing.T) {
	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
	}
	action := actions.CreateAction("delete")
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"delete"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestDeleteAction_ForbiddenByRBAC(t *testing.T) {
	rbac := []backplane.RBACRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
	}
	client := createMockSession(t, clusterC1, rbac)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "ns-a",
		Name:      "pod-1",
	}
	action := actions.CreateAction("delete")

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err == nil {
		t.Fatal("expected error when session only grants get/list but delete is needed")
	}
}

// ---------------------------------------------------------------------------
// describe-nodes action
// ---------------------------------------------------------------------------

func TestDescribeNodes_ListAll(t *testing.T) {
	target := actions.ResourceTarget{
		Version:       "v1",
		Resource:      "nodes",
		ClusterScoped: true,
	}
	action := actions.CreateAction("describe-nodes")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) == 0 {
		t.Fatal("expected at least 1 node description")
	}

	node := result.Resources[0].Object
	if node["name"] != "node-1" {
		t.Errorf("expected node name node-1, got %v", node["name"])
	}

	events, _ := node["events"].([]interface{})
	if len(events) == 0 {
		t.Error("expected correlated events for node-1")
	}

	if node["lease"] == nil {
		t.Error("expected correlated lease for node-1")
	}
}

func TestDescribeNodes_SingleNode(t *testing.T) {
	target := actions.ResourceTarget{
		Version:       "v1",
		Resource:      "nodes",
		ClusterScoped: true,
		Name:          "node-1",
	}
	action := actions.CreateAction("describe-nodes")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 node description, got %d", len(result.Resources))
	}

	node := result.Resources[0].Object
	if node["name"] != "node-1" {
		t.Errorf("expected node name node-1, got %v", node["name"])
	}

	conditions, ok := node["conditions"].([]interface{})
	if !ok || len(conditions) == 0 {
		t.Error("expected conditions on node description")
	}
}

func TestDescribeNodes_StructureValidation(t *testing.T) {
	target := actions.ResourceTarget{
		Version:       "v1",
		Resource:      "nodes",
		ClusterScoped: true,
	}
	action := actions.CreateAction("describe-nodes")
	client := createMockSession(t, clusterC1, action.RequiredRBAC(target))

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	node := result.Resources[0].Object

	conditions, ok := node["conditions"].([]interface{})
	if !ok || len(conditions) == 0 {
		t.Fatal("expected conditions array")
	}
	cond, ok := conditions[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected condition to be a map")
	}
	if cond["type"] != "Ready" {
		t.Errorf("expected condition type Ready, got %v", cond["type"])
	}
	if cond["status"] != "True" {
		t.Errorf("expected condition status True, got %v", cond["status"])
	}

	events, ok := node["events"].([]interface{})
	if !ok || len(events) == 0 {
		t.Fatal("expected events array")
	}
	event, ok := events[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected event to be a map")
	}
	if event["reason"] != "NodeReady" {
		t.Errorf("expected event reason NodeReady, got %v", event["reason"])
	}
	if event["type"] != "Normal" {
		t.Errorf("expected event type Normal, got %v", event["type"])
	}

	lease, ok := node["lease"].(map[string]interface{})
	if !ok {
		t.Fatal("expected lease to be a map")
	}
	if lease["holderIdentity"] != "node-1" {
		t.Errorf("expected lease holderIdentity node-1, got %v", lease["holderIdentity"])
	}
}

// ---------------------------------------------------------------------------
// get-pull-secret-email (forbidden by backplane RBAC policy)
// ---------------------------------------------------------------------------

func TestGetPullSecretEmail_ForbiddenByBackplane(t *testing.T) {
	action := actions.CreateAction("get-pull-secret-email")
	rbac := action.RequiredRBAC(actions.ResourceTarget{})

	msg := createMockSessionExpectError(t, clusterC1, rbac, 400)
	if msg == "" {
		t.Error("expected non-empty error message from backplane")
	}
}

// ---------------------------------------------------------------------------
// executor: authorization
// ---------------------------------------------------------------------------

func TestExecutor_AuthorizationDenied(t *testing.T) {
	exec, _ := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-forbidden",
		},
	})

	if result.Allowed {
		t.Fatal("expected authorization denial")
	}
	if result.Reason == "" {
		t.Error("expected denial reason")
	}
}

func TestExecutor_AuthorizationAllowed(t *testing.T) {
	exec, _ := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-a",
		},
	})

	if !result.Allowed {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.Output == nil {
		t.Fatal("expected non-nil output")
	}
	if len(result.Output.Resources) != 2 {
		t.Errorf("expected 2 pods, got %d", len(result.Output.Resources))
	}
}

func TestExecutor_PatchThroughPipeline(t *testing.T) {
	exec, _ := newTestExecutor(t, []string{"default"}, nil)

	action := actions.CreateAction("patch")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "configmaps",
			Namespace: "default",
			Name:      "test-proxy-mutation",
		},
		Params: map[string]string{"patch": `{"data":{"key1":"updated"}}`},
	})

	if !result.Allowed {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.Output == nil || len(result.Output.Resources) != 1 {
		t.Fatal("expected 1 resource in output")
	}
	if result.Output.Resources[0].GetName() != "test-proxy-mutation" {
		t.Errorf("expected name test-proxy-mutation, got %s", result.Output.Resources[0].GetName())
	}
}

func TestExecutor_DeleteThroughPipeline(t *testing.T) {
	exec, _ := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("delete")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-a",
			Name:      "pod-1",
		},
	})

	if !result.Allowed {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.Output == nil || result.Output.Message == "" {
		t.Error("expected non-empty message from delete")
	}
}

func TestExecutor_ClusterScopedGet(t *testing.T) {
	exec, _ := newTestExecutor(t, nil, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:       "v1",
			Resource:      "namespaces",
			ClusterScoped: true,
		},
	})

	if !result.Allowed {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if len(result.Output.Resources) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(result.Output.Resources))
	}
}

func TestExecutor_SecretsAuthorizationDenied(t *testing.T) {
	exec, auditor := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "secrets",
			Namespace: "ns-a",
			Name:      "secret-2",
		},
	})

	if result.Allowed {
		t.Fatal("expected secrets access denied without allowlist")
	}
	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record for denied secrets access")
	}
	if auditor.Records[0].Decision != audit.DecisionDenied {
		t.Errorf("expected decision denied, got %s", auditor.Records[0].Decision)
	}
}

func TestExecutor_SecretsAuthorizationWithAllowlist(t *testing.T) {
	exec, auditor := newTestExecutor(t, []string{"ns-a"}, []string{"ns-a/secret-1"})

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "secrets",
			Namespace: "ns-a",
			Name:      "secret-1",
		},
	})

	if !result.Allowed {
		t.Fatalf("expected authorization allowed with secrets allowlist, got denied: %s", result.Reason)
	}
	if result.Error == nil {
		t.Fatal("expected backplane error (forbidden policy blocks secrets), got nil")
	}
	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record")
	}
	if auditor.Records[0].Outcome != audit.OutcomeFailure {
		t.Errorf("expected outcome failure (backplane rejects), got %s", auditor.Records[0].Outcome)
	}
}

func TestExecutor_NamespaceRequired(t *testing.T) {
	exec, _ := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:  "v1",
			Resource: "pods",
		},
	})

	if result.Allowed {
		t.Fatal("expected denial when namespace missing for namespaced resource")
	}
}

// ---------------------------------------------------------------------------
// executor: audit logging
// ---------------------------------------------------------------------------

func TestAudit_SuccessRecorded(t *testing.T) {
	exec, auditor := newTestExecutor(t, nil, nil)

	action := actions.CreateAction("get")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-a",
		},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected at least one audit record")
	}

	rec := auditor.Records[0]
	if rec.CallerID != "test-user" {
		t.Errorf("expected caller_id test-user, got %s", rec.CallerID)
	}
	if rec.Action != "get" {
		t.Errorf("expected action get, got %s", rec.Action)
	}
	if rec.ClusterID != clusterC1 {
		t.Errorf("expected cluster_id %s, got %s", clusterC1, rec.ClusterID)
	}
	if rec.Decision != audit.DecisionAllowed {
		t.Errorf("expected decision allowed, got %s", rec.Decision)
	}
	if rec.Outcome != audit.OutcomeSuccess {
		t.Errorf("expected outcome success, got %s", rec.Outcome)
	}
}

func TestAudit_DenialRecorded(t *testing.T) {
	exec, auditor := newTestExecutor(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-denied",
		},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record for denied request")
	}

	rec := auditor.Records[0]
	if rec.Decision != audit.DecisionDenied {
		t.Errorf("expected decision denied, got %s", rec.Decision)
	}
	if rec.Outcome != audit.OutcomeSkipped {
		t.Errorf("expected outcome skipped, got %s", rec.Outcome)
	}
}

func TestAudit_PatchRecorded(t *testing.T) {
	exec, auditor := newTestExecutor(t, []string{"default"}, nil)

	action := actions.CreateAction("patch")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "configmaps",
			Namespace: "default",
			Name:      "test-proxy-mutation",
		},
		Params: map[string]string{"patch": `{"data":{"key1":"updated"}}`},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record for patch")
	}

	rec := auditor.Records[0]
	if rec.Action != "patch" {
		t.Errorf("expected action patch, got %s", rec.Action)
	}
	if rec.Decision != audit.DecisionAllowed {
		t.Errorf("expected decision allowed, got %s", rec.Decision)
	}
	if rec.Outcome != audit.OutcomeSuccess {
		t.Errorf("expected outcome success, got %s", rec.Outcome)
	}
}

func TestAudit_BackplaneErrorRecorded(t *testing.T) {
	exec, auditor := newTestExecutor(t, []string{"ns-a"}, []string{"ns-a/secret-1"})

	action := actions.CreateAction("get")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "secrets",
			Namespace: "ns-a",
			Name:      "secret-1",
		},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record for backplane error")
	}

	rec := auditor.Records[0]
	if rec.Decision != audit.DecisionAllowed {
		t.Errorf("expected decision allowed (authorizer passed), got %s", rec.Decision)
	}
	if rec.Outcome != audit.OutcomeFailure {
		t.Errorf("expected outcome failure (backplane rejected), got %s", rec.Outcome)
	}
	if rec.Error == "" {
		t.Error("expected error message in audit record")
	}
}

// ---------------------------------------------------------------------------
// proxy: action-level tests
// ---------------------------------------------------------------------------

func TestGetAction_Proxy_ListNamespaces(t *testing.T) {
	client := createProxySession(t, clusterC1)

	target := actions.ResourceTarget{
		Version:       "v1",
		Resource:      "namespaces",
		ClusterScoped: true,
	}
	action := actions.CreateAction("get")

	result, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Resources) == 0 {
		t.Fatal("expected at least 1 namespace")
	}
}

func TestGetAction_Proxy_ListPods(t *testing.T) {
	client := createProxySession(t, clusterC1)

	target := actions.ResourceTarget{
		Version:   "v1",
		Resource:  "pods",
		Namespace: "default",
	}
	action := actions.CreateAction("get")

	_, err := action.Execute(context.Background(), actions.Clients{Dynamic: client}, actions.ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// ---------------------------------------------------------------------------
// proxy: executor-level tests
// ---------------------------------------------------------------------------

func TestExecutor_Proxy_AuthorizationDenied(t *testing.T) {
	exec, _ := newTestExecutorWithProxy(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-forbidden",
		},
	})

	if result.Allowed {
		t.Fatal("expected authorization denial")
	}
	if result.Reason == "" {
		t.Error("expected denial reason")
	}
}

func TestExecutor_Proxy_ClusterScopedGet(t *testing.T) {
	exec, _ := newTestExecutorWithProxy(t, nil, nil)

	action := actions.CreateAction("get")
	result := exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:       "v1",
			Resource:      "namespaces",
			ClusterScoped: true,
		},
	})

	if !result.Allowed {
		t.Fatalf("expected authorized, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.Output == nil || len(result.Output.Resources) == 0 {
		t.Fatal("expected at least 1 namespace")
	}
}

func TestAudit_Proxy_SuccessRecorded(t *testing.T) {
	exec, auditor := newTestExecutorWithProxy(t, nil, nil)

	action := actions.CreateAction("get")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:       "v1",
			Resource:      "namespaces",
			ClusterScoped: true,
		},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected at least one audit record")
	}
	rec := auditor.Records[0]
	if rec.Decision != audit.DecisionAllowed {
		t.Errorf("expected decision allowed, got %s", rec.Decision)
	}
	if rec.Outcome != audit.OutcomeSuccess {
		t.Errorf("expected outcome success, got %s", rec.Outcome)
	}
}

func TestAudit_Proxy_DenialRecorded(t *testing.T) {
	exec, auditor := newTestExecutorWithProxy(t, []string{"ns-a"}, nil)

	action := actions.CreateAction("get")
	exec.Execute(context.Background(), executor.Request{
		CallerID:  "test-user",
		ClusterID: clusterC1,
		Action:    action,
		Target: actions.ResourceTarget{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "ns-denied",
		},
	})

	if len(auditor.Records) == 0 {
		t.Fatal("expected audit record for denied request")
	}
	rec := auditor.Records[0]
	if rec.Decision != audit.DecisionDenied {
		t.Errorf("expected decision denied, got %s", rec.Decision)
	}
}
