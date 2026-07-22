package actions

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

func newConfigMap(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"namespace": "openshift-monitoring",
				"name":      name,
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}
}

var configMapTarget = ResourceTarget{
	Group:     "",
	Version:   "v1",
	Resource:  "configmaps",
	Namespace: "openshift-monitoring",
}

// GET action tests

func TestGetAction_Name(t *testing.T) {
	action := NewGetAction()
	if action.Name() != "get" {
		t.Errorf("expected name %q, got %q", "get", action.Name())
	}
}

func TestGetAction_RequiredRBAC_Get(t *testing.T) {
	action := NewGetAction()
	target := configMapTarget
	target.Name = "my-config"

	rules := action.RequiredRBAC(target)
	if len(rules) != 1 {
		t.Fatalf("expected 1 RBAC rule, got %d", len(rules))
	}
	if rules[0].Verbs[0] != "get" {
		t.Errorf("expected verb %q, got %q", "get", rules[0].Verbs[0])
	}
}

func TestGetAction_RequiredRBAC_List(t *testing.T) {
	action := NewGetAction()
	target := configMapTarget

	rules := action.RequiredRBAC(target)
	if len(rules) != 1 {
		t.Fatalf("expected 1 RBAC rule, got %d", len(rules))
	}
	if rules[0].Verbs[0] != "list" {
		t.Errorf("expected verb %q, got %q", "list", rules[0].Verbs[0])
	}
}

func TestGetAction_SingleResource(t *testing.T) {
	cm := newConfigMap("cluster-config")
	client := newFakeClient(cm)
	action := NewGetAction()

	target := configMapTarget
	target.Name = "cluster-config"

	result, err := action.Execute(context.Background(), client, ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].GetName() != "cluster-config" {
		t.Errorf("expected name %q, got %q", "cluster-config", result.Resources[0].GetName())
	}
}

func TestGetAction_ListResources(t *testing.T) {
	cm1 := newConfigMap("config-1")
	cm2 := newConfigMap("config-2")
	client := newFakeClient(cm1, cm2)
	action := NewGetAction()

	result, err := action.Execute(context.Background(), client, ActionRequest{Target: configMapTarget})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(result.Resources))
	}
}

func TestGetAction_NotFound(t *testing.T) {
	client := newFakeClient()
	action := NewGetAction()

	target := configMapTarget
	target.Name = "nonexistent"

	_, err := action.Execute(context.Background(), client, ActionRequest{Target: target})
	if err == nil {
		t.Error("expected error for non-existent resource, got nil")
	}
}

// PATCH action tests

func TestPatchAction_Name(t *testing.T) {
	action := NewPatchAction()
	if action.Name() != "patch" {
		t.Errorf("expected name %q, got %q", "patch", action.Name())
	}
}

func TestPatchAction_RequiredRBAC(t *testing.T) {
	action := NewPatchAction()
	target := configMapTarget
	target.Name = "my-config"

	rules := action.RequiredRBAC(target)
	if len(rules) != 1 {
		t.Fatalf("expected 1 RBAC rule, got %d", len(rules))
	}
	if rules[0].Verbs[0] != "patch" {
		t.Errorf("expected verb %q, got %q", "patch", rules[0].Verbs[0])
	}
}

func TestPatchAction_Success(t *testing.T) {
	cm := newConfigMap("cluster-config")
	client := newFakeClient(cm)
	action := NewPatchAction()

	target := configMapTarget
	target.Name = "cluster-config"

	result, err := action.Execute(context.Background(), client, ActionRequest{
		Target: target,
		Params: map[string]string{
			"patch": `{"data":{"key":"updated"}}`,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
}

func TestPatchAction_MissingName(t *testing.T) {
	client := newFakeClient()
	action := NewPatchAction()

	_, err := action.Execute(context.Background(), client, ActionRequest{
		Target: configMapTarget,
		Params: map[string]string{"patch": `{}`},
	})
	if err == nil {
		t.Error("expected error for missing name, got nil")
	}
}

func TestPatchAction_MissingPatchParam(t *testing.T) {
	client := newFakeClient()
	action := NewPatchAction()

	target := configMapTarget
	target.Name = "cluster-config"

	_, err := action.Execute(context.Background(), client, ActionRequest{Target: target})
	if err == nil {
		t.Error("expected error for missing patch param, got nil")
	}
}

func TestPatchAction_NotFound(t *testing.T) {
	client := newFakeClient()
	action := NewPatchAction()

	target := configMapTarget
	target.Name = "nonexistent"

	_, err := action.Execute(context.Background(), client, ActionRequest{
		Target: target,
		Params: map[string]string{"patch": `{"data":{"key":"value"}}`},
	})
	if err == nil {
		t.Error("expected error for non-existent resource, got nil")
	}
}

// DELETE action tests

func TestDeleteAction_Name(t *testing.T) {
	action := NewDeleteAction()
	if action.Name() != "delete" {
		t.Errorf("expected name %q, got %q", "delete", action.Name())
	}
}

func TestDeleteAction_RequiredRBAC(t *testing.T) {
	action := NewDeleteAction()
	target := configMapTarget
	target.Name = "my-config"

	rules := action.RequiredRBAC(target)
	if len(rules) != 1 {
		t.Fatalf("expected 1 RBAC rule, got %d", len(rules))
	}
	if rules[0].Verbs[0] != "delete" {
		t.Errorf("expected verb %q, got %q", "delete", rules[0].Verbs[0])
	}
}

func TestDeleteAction_Success(t *testing.T) {
	cm := newConfigMap("to-delete")
	client := newFakeClient(cm)
	action := NewDeleteAction()

	target := configMapTarget
	target.Name = "to-delete"

	result, err := action.Execute(context.Background(), client, ActionRequest{Target: target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}

	// Verify resource is actually deleted
	getAction := NewGetAction()
	_, err = getAction.Execute(context.Background(), client, ActionRequest{Target: target})
	if err == nil {
		t.Error("expected error after delete, resource should be gone")
	}
}

func TestDeleteAction_MissingName(t *testing.T) {
	client := newFakeClient()
	action := NewDeleteAction()

	_, err := action.Execute(context.Background(), client, ActionRequest{Target: configMapTarget})
	if err == nil {
		t.Error("expected error for missing name, got nil")
	}
}

func TestDeleteAction_NotFound(t *testing.T) {
	client := newFakeClient()
	action := NewDeleteAction()

	target := configMapTarget
	target.Name = "nonexistent"

	_, err := action.Execute(context.Background(), client, ActionRequest{Target: target})
	if err == nil {
		t.Error("expected error for non-existent resource, got nil")
	}
}

// Verify all actions declare correct resource in RBAC rules

func TestActions_RBACResourceMatchesTarget(t *testing.T) {
	actions := []Action{NewGetAction(), NewPatchAction(), NewDeleteAction()}
	target := ResourceTarget{
		Group:     "apps",
		Version:   "v1",
		Resource:  "deployments",
		Namespace: "openshift-monitoring",
		Name:      "prometheus",
	}

	for _, action := range actions {
		t.Run(action.Name(), func(t *testing.T) {
			rules := action.RequiredRBAC(target)
			if len(rules) == 0 {
				t.Fatal("expected at least 1 RBAC rule")
			}
			if rules[0].APIGroups[0] != "apps" {
				t.Errorf("expected API group %q, got %q", "apps", rules[0].APIGroups[0])
			}
			if rules[0].Resources[0] != "deployments" {
				t.Errorf("expected resource %q, got %q", "deployments", rules[0].Resources[0])
			}
			if len(rules[0].ResourceNames) != 1 || rules[0].ResourceNames[0] != "prometheus" {
				t.Errorf("expected resource name %q, got %v", "prometheus", rules[0].ResourceNames)
			}
		})
	}
}

func TestGetAction_RequiredRBAC_ListOmitsResourceNames(t *testing.T) {
	action := NewGetAction()
	rules := action.RequiredRBAC(configMapTarget)
	if len(rules[0].ResourceNames) != 0 {
		t.Errorf("expected no resource names for list, got %v", rules[0].ResourceNames)
	}
}
