package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"k8s.io/client-go/dynamic"
)

type fakeClientProvider struct {
	client dynamic.Interface
	err    error
}

func (f *fakeClientProvider) GetClient(_ context.Context, _, _ string, _ []backplane.RBACRule) (dynamic.Interface, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.client, nil
}

func (f *fakeClientProvider) GetPodExecutor(_ context.Context, _, _ string, _ []backplane.RBACRule) (backplane.PodExecutor, error) {
	return nil, fmt.Errorf("not implemented in test fake")
}

func newTestExecutor(namespaces, secrets []string, bp backplane.ClientProvider) (*Executor, *audit.MockLogger) {
	logger := logrus.New()
	auditor := audit.NewMockLogger(logger)
	authz := authorization.New(logger, namespaces, secrets)
	return New(logger, auditor, NewKubeScope(authz, bp)), auditor
}

func newFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
}

func newConfigMap(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

var testTarget = actions.ResourceTarget{
	Group:     "",
	Version:   "v1",
	Resource:  "configmaps",
	Namespace: "openshift-monitoring",
	Name:      "cluster-config",
}

func TestExecutor_HappyPath_Get(t *testing.T) {
	cm := newConfigMap("openshift-monitoring", "cluster-config")
	bp := &fakeClientProvider{client: newFakeClient(cm)}
	exec, auditor := newTestExecutor([]string{"openshift-monitoring"}, nil, bp)

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    testTarget,
	})

	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.Output == nil {
		t.Fatal("expected output, got nil")
	}
	if len(result.Output.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(result.Output.Resources))
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Decision != audit.DecisionAllowed {
		t.Errorf("expected audit decision %q, got %q", audit.DecisionAllowed, auditor.Records[0].Decision)
	}
	if auditor.Records[0].Outcome != audit.OutcomeSuccess {
		t.Errorf("expected audit outcome %q, got %q", audit.OutcomeSuccess, auditor.Records[0].Outcome)
	}
}

func TestExecutor_DeniedNamespace(t *testing.T) {
	bp := &fakeClientProvider{client: newFakeClient()}
	exec, auditor := newTestExecutor([]string{"openshift-logging"}, nil, bp)

	target := testTarget
	target.Namespace = "customer-namespace"

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    target,
	})

	if result.Allowed {
		t.Error("expected denied, got allowed")
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Decision != audit.DecisionDenied {
		t.Errorf("expected audit decision %q, got %q", audit.DecisionDenied, auditor.Records[0].Decision)
	}
	if auditor.Records[0].Outcome != audit.OutcomeSkipped {
		t.Errorf("expected audit outcome %q, got %q", audit.OutcomeSkipped, auditor.Records[0].Outcome)
	}
}

func TestExecutor_DeniedSecret(t *testing.T) {
	bp := &fakeClientProvider{client: newFakeClient()}
	exec, auditor := newTestExecutor([]string{"openshift-monitoring"}, nil, bp)

	target := actions.ResourceTarget{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "openshift-monitoring",
		Name:      "some-secret",
	}

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    target,
	})

	if result.Allowed {
		t.Error("expected secret to be denied, got allowed")
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Decision != audit.DecisionDenied {
		t.Errorf("expected audit decision %q, got %q", audit.DecisionDenied, auditor.Records[0].Decision)
	}
}

func TestExecutor_AllowedSecret(t *testing.T) {
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"namespace": "openshift-monitoring",
				"name":      "alertmanager-config",
			},
		},
	}
	bp := &fakeClientProvider{client: newFakeClient(secret)}
	exec, auditor := newTestExecutor(
		[]string{"openshift-monitoring"},
		[]string{"openshift-monitoring/alertmanager-config"},
		bp,
	)

	target := actions.ResourceTarget{
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Namespace: "openshift-monitoring",
		Name:      "alertmanager-config",
	}

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    target,
	})

	if !result.Allowed {
		t.Errorf("expected allowed for allow-listed secret, got denied: %s", result.Reason)
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Decision != audit.DecisionAllowed {
		t.Errorf("expected audit decision %q, got %q", audit.DecisionAllowed, auditor.Records[0].Decision)
	}
}

func TestExecutor_BackplaneError(t *testing.T) {
	bp := &fakeClientProvider{err: fmt.Errorf("backplane unavailable")}
	exec, auditor := newTestExecutor([]string{"openshift-monitoring"}, nil, bp)

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    testTarget,
	})

	if !result.Allowed {
		t.Error("expected allowed (auth passed), got denied")
	}
	if result.Error == nil {
		t.Error("expected error from backplane failure, got nil")
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Outcome != audit.OutcomeFailure {
		t.Errorf("expected audit outcome %q, got %q", audit.OutcomeFailure, auditor.Records[0].Outcome)
	}
}

func TestExecutor_ActionError(t *testing.T) {
	bp := &fakeClientProvider{client: newFakeClient()}
	exec, auditor := newTestExecutor([]string{"openshift-monitoring"}, nil, bp)

	target := testTarget
	target.Name = "nonexistent"

	result := exec.Execute(context.Background(), Request{
		CallerID:  "test-user",
		ClusterID: "cluster-123",
		Action:    actions.NewGetAction(),
		Target:    target,
	})

	if !result.Allowed {
		t.Error("expected allowed (auth passed), got denied")
	}
	if result.Error == nil {
		t.Error("expected error from action failure, got nil")
	}
	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}
	if auditor.Records[0].Outcome != audit.OutcomeFailure {
		t.Errorf("expected audit outcome %q, got %q", audit.OutcomeFailure, auditor.Records[0].Outcome)
	}
}

func TestExecutor_AuditRecordFields(t *testing.T) {
	cm := newConfigMap("openshift-monitoring", "cluster-config")
	bp := &fakeClientProvider{client: newFakeClient(cm)}
	exec, auditor := newTestExecutor([]string{"openshift-monitoring"}, nil, bp)

	exec.Execute(context.Background(), Request{
		CallerID:  "srep-user",
		ClusterID: "cluster-456",
		Action:    actions.NewGetAction(),
		Target:    testTarget,
	})

	if len(auditor.Records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.Records))
	}

	rec := auditor.Records[0]
	if rec.CallerID != "srep-user" {
		t.Errorf("expected caller %q, got %q", "srep-user", rec.CallerID)
	}
	if rec.ClusterID != "cluster-456" {
		t.Errorf("expected cluster %q, got %q", "cluster-456", rec.ClusterID)
	}
	if rec.Action != "get" {
		t.Errorf("expected action %q, got %q", "get", rec.Action)
	}
	target, ok := rec.Target.(actions.ResourceTarget)
	if !ok {
		t.Fatalf("expected a Kubernetes resource target, got %T", rec.Target)
	}
	if target.Resource != "configmaps" {
		t.Errorf("expected resource type %q, got %q", "configmaps", target.Resource)
	}
	if target.Namespace != "openshift-monitoring" {
		t.Errorf("expected namespace %q, got %q", "openshift-monitoring", target.Namespace)
	}
	if target.Name != "cluster-config" {
		t.Errorf("expected name %q, got %q", "cluster-config", target.Name)
	}
}

// A Failure carrying no cause must render and audit as an error, not panic.
// Nothing constructs one today, but both Error and auditedCause dereferenced
// Cause unconditionally, so the first one that did would have taken down the
// request it was reporting on.
func TestFailure_WithoutCause(t *testing.T) {
	failure := &Failure{Message: "refused"}

	if got := failure.Error(); got != "refused" {
		t.Errorf("Error() = %q, want %q", got, "refused")
	}
	if got := auditedCause(failure); got == nil || got.Error() != "refused" {
		t.Errorf("auditedCause() = %v, want an error reading %q", got, "refused")
	}
}

func TestFailure_WithCause(t *testing.T) {
	cause := errors.New("boom")
	failure := &Failure{Message: "refused", Cause: cause}

	if got := failure.Error(); got != "refused: boom" {
		t.Errorf("Error() = %q, want %q", got, "refused: boom")
	}
	// The cause is what gets audited, so the record names the underlying
	// problem rather than the layer that wrapped it.
	if got := auditedCause(failure); got != cause {
		t.Errorf("auditedCause() = %v, want the wrapped cause", got)
	}
}
