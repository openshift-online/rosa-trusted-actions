package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

type fakePodExecutor struct {
	responses [][]byte
	callIndex int
}

func (f *fakePodExecutor) Exec(_ context.Context, _, _, _ string, _ []string) ([]byte, error) {
	idx := f.callIndex
	f.callIndex++
	if idx >= len(f.responses) {
		return nil, fmt.Errorf("unexpected exec call %d", idx)
	}
	return f.responses[idx], nil
}

func newAlertsFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		podsGVR: "PodList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objects...)
}

func newPrometheusPod() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "prometheus-k8s-0",
				"namespace": "openshift-monitoring",
				"labels": map[string]interface{}{
					"app.kubernetes.io/name": "prometheus",
				},
			},
		},
	}
}

func newAlertmanagerPod() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "alertmanager-main-0",
				"namespace": "openshift-monitoring",
				"labels": map[string]interface{}{
					"app.kubernetes.io/name": "alertmanager",
				},
			},
		},
	}
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return raw
}

func prometheusResponse(alerts ...map[string]interface{}) map[string]interface{} {
	results := make([]interface{}, 0, len(alerts))
	for _, a := range alerts {
		results = append(results, map[string]interface{}{
			"metric": a,
		})
	}
	return map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "vector",
			"result":     results,
		},
	}
}

func alertmanagerSilencesResponse(silences ...map[string]interface{}) []interface{} {
	data := make([]interface{}, 0, len(silences))
	for _, s := range silences {
		data = append(data, s)
	}
	return data
}

func extractAlertCounts(t *testing.T, result *ActionResult) ([]interface{}, []interface{}) {
	t.Helper()
	alerts, ok := result.Resources[0].Object["alerts"].(map[string]interface{})
	if !ok {
		t.Fatal("expected alerts to be a map")
	}
	critical, ok := alerts["critical"].([]interface{})
	if !ok {
		t.Fatal("expected critical to be a slice")
	}
	warning, ok := alerts["warning"].([]interface{})
	if !ok {
		t.Fatal("expected warning to be a slice")
	}
	return critical, warning
}

func TestListAlertsAction_Name(t *testing.T) {
	action := NewListAlertsAction()
	if action.Name() != "list-alerts" {
		t.Errorf("expected name %q, got %q", "list-alerts", action.Name())
	}
}

func TestListAlertsAction_RequiredRBAC(t *testing.T) {
	action := NewListAlertsAction()
	rules := action.RequiredRBAC(ResourceTarget{Namespace: "openshift-monitoring"})

	if len(rules) != 2 {
		t.Fatalf("expected 2 RBAC rules, got %d", len(rules))
	}

	if rules[0].Resources[0] != "pods" {
		t.Errorf("expected resource %q, got %q", "pods", rules[0].Resources[0])
	}
	if rules[0].Verbs[0] != "list" {
		t.Errorf("expected verb %q, got %q", "list", rules[0].Verbs[0])
	}
	if rules[1].Resources[0] != "pods/exec" {
		t.Errorf("expected resource %q, got %q", "pods/exec", rules[1].Resources[0])
	}
	if rules[1].Verbs[0] != "create" {
		t.Errorf("expected verb %q, got %q", "create", rules[1].Verbs[0])
	}
}

func TestListAlertsAction_Execute_NoPodExecutor(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	action := NewListAlertsAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
	})
	if err == nil {
		t.Fatal("expected error when pod executor is nil, got nil")
	}
}

func TestListAlertsAction_Execute_InvalidSeverity(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	executor := &fakePodExecutor{}
	action := NewListAlertsAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{"severity": "info"},
	})
	if err == nil {
		t.Fatal("expected error for invalid severity, got nil")
	}
	if !strings.Contains(err.Error(), "invalid severity") {
		t.Errorf("expected 'invalid severity' error, got: %v", err)
	}
}

func TestListAlertsAction_Execute_InvalidState(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	executor := &fakePodExecutor{}
	action := NewListAlertsAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{"state": "bogus"},
	})
	if err == nil {
		t.Fatal("expected error for invalid state, got nil")
	}
	if !strings.Contains(err.Error(), "invalid alert state") {
		t.Errorf("expected 'invalid alert state' error, got: %v", err)
	}
}

func TestListAlertsAction_Execute_NoPrometheusPod(t *testing.T) {
	client := newAlertsFakeClient()
	executor := &fakePodExecutor{}
	action := NewListAlertsAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
	})
	if err == nil {
		t.Fatal("expected error for missing prometheus pod, got nil")
	}
}

func TestListAlertsAction_Execute_FiringAlerts(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	promResp := mustMarshal(t, prometheusResponse(
		map[string]interface{}{"alertname": "KubeNodeNotReady", "severity": "critical", "namespace": "openshift-monitoring", "alertstate": "firing"},
		map[string]interface{}{"alertname": "KubePodCrashLooping", "severity": "warning", "namespace": "openshift-monitoring", "alertstate": "firing"},
		map[string]interface{}{"alertname": "ClusterOperatorDown", "severity": "critical", "namespace": "openshift-cluster-version", "alertstate": "firing"},
	))

	executor := &fakePodExecutor{responses: [][]byte{promResp}}
	action := NewListAlertsAction()
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}

	alerts, ok := result.Resources[0].Object["alerts"].(map[string]interface{})
	if !ok {
		t.Fatal("expected alerts to be a map")
	}

	critical, ok := alerts["critical"].([]interface{})
	if !ok {
		t.Fatal("expected critical to be a slice")
	}
	if len(critical) != 2 {
		t.Errorf("expected 2 critical alerts, got %d", len(critical))
	}

	warning, ok := alerts["warning"].([]interface{})
	if !ok {
		t.Fatal("expected warning to be a slice")
	}
	if len(warning) != 1 {
		t.Errorf("expected 1 warning alert, got %d", len(warning))
	}
}

func TestListAlertsAction_Execute_SeverityFilter(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	promResp := mustMarshal(t, prometheusResponse(
		map[string]interface{}{"alertname": "KubeNodeNotReady", "severity": "critical", "alertstate": "firing"},
		map[string]interface{}{"alertname": "KubePodCrashLooping", "severity": "warning", "alertstate": "firing"},
	))

	action := NewListAlertsAction()

	// Critical only
	executor := &fakePodExecutor{responses: [][]byte{promResp}}
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{"severity": "critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	critical, warning := extractAlertCounts(t, result)
	if len(critical) != 1 {
		t.Errorf("expected 1 critical alert, got %d", len(critical))
	}
	if len(warning) != 0 {
		t.Errorf("expected 0 warning alerts, got %d", len(warning))
	}

	// Warning only
	executor = &fakePodExecutor{responses: [][]byte{promResp}}
	result, err = action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{"severity": "warning"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	critical, warning = extractAlertCounts(t, result)
	if len(critical) != 0 {
		t.Errorf("expected 0 critical alerts, got %d", len(critical))
	}
	if len(warning) != 1 {
		t.Errorf("expected 1 warning alert, got %d", len(warning))
	}
}

func TestListAlertsAction_Execute_AlertFields(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	promResp := mustMarshal(t, prometheusResponse(
		map[string]interface{}{
			"alertname":          "KubeNodeNotReady",
			"severity":           "critical",
			"alertstate":         "firing",
			"job":                "kube-state-metrics",
			"namespace":          "openshift-monitoring",
			"exported_namespace": "default",
			"pod":                "node-exporter-abc",
			"service":            "node-exporter",
			"__name__":           "ALERTS",
			"prometheus":         "openshift-monitoring/k8s",
		},
	))

	executor := &fakePodExecutor{responses: [][]byte{promResp}}
	action := NewListAlertsAction()
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	critical, _ := extractAlertCounts(t, result)
	if len(critical) != 1 {
		t.Fatalf("expected 1 critical alert, got %d", len(critical))
	}

	alert, ok := critical[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected alert to be a map")
	}

	expectedFields := []string{"alertname", "job", "namespace", "exported_namespace", "pod", "service", "alertstate"}
	for _, field := range expectedFields {
		if alert[field] == nil {
			t.Errorf("expected field %q to be present", field)
		}
	}

	unexpectedFields := []string{"__name__", "prometheus", "severity"}
	for _, field := range unexpectedFields {
		if alert[field] != nil {
			t.Errorf("unexpected field %q present in alert", field)
		}
	}
}

func TestListAlertsAction_Execute_WithSilences(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod(), newAlertmanagerPod())

	promResp := mustMarshal(t, prometheusResponse(
		map[string]interface{}{"alertname": "Watchdog", "severity": "critical", "alertstate": "firing"},
	))
	silencesResp := mustMarshal(t, alertmanagerSilencesResponse(
		map[string]interface{}{
			"id":        "silence-1",
			"createdBy": "admin@example.com",
			"comment":   "Maintenance window",
			"startsAt":  "2024-01-15T10:00:00Z",
			"endsAt":    "2024-01-15T12:00:00Z",
			"matchers":  []interface{}{map[string]interface{}{"name": "alertname", "value": "Watchdog"}},
			"status":    map[string]interface{}{"state": "active"},
		},
		map[string]interface{}{
			"id":        "silence-2",
			"createdBy": "admin@example.com",
			"comment":   "Expired",
			"startsAt":  "2024-01-14T10:00:00Z",
			"endsAt":    "2024-01-14T12:00:00Z",
			"status":    map[string]interface{}{"state": "expired"},
		},
	))

	executor := &fakePodExecutor{responses: [][]byte{promResp, silencesResp}}
	action := NewListAlertsAction()
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{"silences": "true"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	silences, ok := result.Resources[0].Object["silences"].([]interface{})
	if !ok {
		t.Fatal("expected silences to be a slice")
	}
	if len(silences) != 1 {
		t.Fatalf("expected 1 active silence, got %d", len(silences))
	}

	silence, ok := silences[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected silence to be a map")
	}

	if silence["id"] != "silence-1" {
		t.Errorf("expected silence id %q, got %v", "silence-1", silence["id"])
	}
	if silence["createdBy"] != "admin@example.com" {
		t.Errorf("expected createdBy %q, got %v", "admin@example.com", silence["createdBy"])
	}
	if silence["status"] != nil {
		t.Error("expected status to be stripped from silence")
	}
}

func TestListAlertsAction_Execute_EmptyAlerts(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	promResp := mustMarshal(t, prometheusResponse())

	executor := &fakePodExecutor{responses: [][]byte{promResp}}
	action := NewListAlertsAction()
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{Namespace: "openshift-monitoring"},
		Params: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	critical, warning := extractAlertCounts(t, result)
	if len(critical) != 0 {
		t.Errorf("expected 0 critical alerts, got %d", len(critical))
	}
	if len(warning) != 0 {
		t.Errorf("expected 0 warning alerts, got %d", len(warning))
	}

	if result.Message != "0 critical, 0 warning alerts" {
		t.Errorf("expected message %q, got %q", "0 critical, 0 warning alerts", result.Message)
	}
}

func TestListAlertsAction_Execute_DefaultNamespace(t *testing.T) {
	client := newAlertsFakeClient(newPrometheusPod())
	promResp := mustMarshal(t, prometheusResponse())

	executor := &fakePodExecutor{responses: [][]byte{promResp}}
	action := NewListAlertsAction()
	result, err := action.Execute(context.Background(), Clients{Dynamic: client, PodExecutor: executor}, ActionRequest{
		Target: ResourceTarget{},
		Params: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(result.Resources))
	}
}

func TestBuildAlertsQuery(t *testing.T) {
	tests := []struct {
		state    string
		contains string
	}{
		{"firing", `alertstate="firing"`},
		{"pending", `alertstate="pending"`},
		{"all", alertsNamespaceQuery},
	}

	for _, tc := range tests {
		query := buildAlertsQuery(tc.state)
		if !strings.Contains(query, tc.contains) {
			t.Errorf("state=%q: expected query to contain %q, got %q", tc.state, tc.contains, query)
		}
	}
}