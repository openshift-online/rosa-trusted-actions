package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"k8s.io/client-go/dynamic"

	"github.com/openshift-online/rosa-trusted-actions/internal/audit"
	"github.com/openshift-online/rosa-trusted-actions/internal/authorization"
	"github.com/openshift-online/rosa-trusted-actions/internal/backplane"
	"github.com/openshift-online/rosa-trusted-actions/internal/executor"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

type fakeClientProvider struct {
	client dynamic.Interface
	err    error
}

func (f *fakeClientProvider) GetClient(_ context.Context, _ string, _ []backplane.RBACRule) (dynamic.Interface, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.client, nil
}

func (f *fakeClientProvider) GetPodExecutor(_ context.Context, _ string, _ []backplane.RBACRule) (backplane.PodExecutor, error) {
	return nil, fmt.Errorf("not implemented in test fake")
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

func newTestRunner(namespaces []string, bp backplane.ClientProvider) *ExecutorRunner {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	authz := authorization.New(logger, namespaces, nil)
	auditor := audit.NewMockLogger(logger)
	exec := executor.New(logger, authz, auditor, bp)
	return NewExecutorRunner(logger, exec, 5*time.Second)
}

func testExecution(action string, params map[string]string) *models.Execution {
	raw, _ := json.Marshal(params)
	rawMsg := json.RawMessage(raw)
	username := "srep-user"
	return &models.Execution{
		ID:            uuid.New(),
		Action:        action,
		Status:        "running",
		Username:      &username,
		TargetCluster: "cluster-123",
		Params:        &rawMsg,
	}
}

func TestExecutorRunner_Run_Success(t *testing.T) {
	cm := newConfigMap("openshift-monitoring", "cluster-config")
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), cm)}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("get", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": "openshift-monitoring",
		"name":      "cluster-config",
	})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "succeeded" {
		t.Errorf("expected status %q, got %q", "succeeded", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason != "" {
		t.Errorf("expected empty reason on success, got %q", reason)
	}
}

func TestExecutorRunner_Run_Denied(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-logging"}, bp)

	exec := testExecution("get", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": "customer-namespace",
		"name":      "cluster-config",
	})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason on denial")
	}
}

// TestExecutorRunner_Run_MissingNamespaceDenied guards against inferring
// ClusterScoped from an omitted namespace param: a caller targeting a
// namespaced resource without a namespace must be denied by the namespace
// allowlist, not silently treated as a cluster-scoped (and thus
// allowlist-exempt) request.
func TestExecutorRunner_Run_MissingNamespaceDenied(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("get", map[string]string{
		"version":  "v1",
		"resource": "configmaps",
		// namespace intentionally omitted
		"name": "cluster-config",
	})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q (denied, not treated as cluster-scoped), got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason on denial")
	}
}

func TestExecutorRunner_Run_ActionError(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("get", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": "openshift-monitoring",
		"name":      "nonexistent",
	})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason on action error")
	}
}

func TestExecutorRunner_Run_BackplaneError(t *testing.T) {
	bp := &fakeClientProvider{err: context.DeadlineExceeded}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("get", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": "openshift-monitoring",
	})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason on backplane error")
	}
}

// slowClientProvider blocks until its context is cancelled, simulating a hung
// backplane/cluster call.
type slowClientProvider struct{}

func (s *slowClientProvider) GetClient(ctx context.Context, _ string, _ []backplane.RBACRule) (dynamic.Interface, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *slowClientProvider) GetPodExecutor(_ context.Context, _ string, _ []backplane.RBACRule) (backplane.PodExecutor, error) {
	return nil, fmt.Errorf("not implemented in test fake")
}

func TestExecutorRunner_Run_RespectsExecutionTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	authz := authorization.New(logger, []string{"openshift-monitoring"}, nil)
	auditor := audit.NewMockLogger(logger)
	exec := executor.New(logger, authz, auditor, &slowClientProvider{})
	runner := NewExecutorRunner(logger, exec, 20*time.Millisecond)

	e := testExecution("get", map[string]string{
		"version":   "v1",
		"resource":  "configmaps",
		"namespace": "openshift-monitoring",
	})

	start := time.Now()
	// A caller context that never cancels on its own — Run must still return
	// promptly once its own executionTimeout elapses.
	status, completedAt, reason := runner.Run(context.Background(), e)
	elapsed := time.Since(start)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason on timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected Run to return once the execution timeout elapsed, took %s", elapsed)
	}
}

func TestExecutorRunner_Run_UnknownAction(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("unknown-action", map[string]string{})

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason for unknown action")
	}
}

func TestExecutorRunner_Run_MalformedParams(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	exec := testExecution("get", nil)
	badParams := json.RawMessage(`{"not":"a string map",`)
	exec.Params = &badParams

	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason for malformed params")
	}
}

func TestExecutorRunner_Run_NilParams(t *testing.T) {
	bp := &fakeClientProvider{client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
	runner := newTestRunner([]string{"openshift-monitoring"}, bp)

	// delete requires a resource name and errors before touching the client,
	// so this exercises decodeParams(nil) without depending on fake client
	// GVR registration.
	exec := &models.Execution{
		ID:            uuid.New(),
		Action:        "delete",
		Status:        "running",
		TargetCluster: "cluster-123",
		Params:        nil,
	}

	// Must not panic on a nil Params field.
	status, completedAt, reason := runner.Run(context.Background(), exec)

	if status != "failed" {
		t.Errorf("expected status %q, got %q", "failed", status)
	}
	if completedAt == nil {
		t.Fatal("expected non-nil completedAt")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}
