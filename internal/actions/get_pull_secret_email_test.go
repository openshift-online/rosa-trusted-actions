package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newPullSecretFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		secretsGVR: "SecretList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objects...)
}

func newPullSecret(email string) *unstructured.Unstructured {
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"cloud.openshift.com": map[string]interface{}{
				"auth":  "dGVzdDp0ZXN0",
				"email": email,
			},
			"registry.redhat.io": map[string]interface{}{
				"auth":  "dGVzdDp0ZXN0",
				"email": email,
			},
		},
	}
	raw, err := json.Marshal(dockerConfig)
	if err != nil {
		panic("failed to marshal dockerConfig fixture: " + err.Error())
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				".dockerconfigjson": encoded,
			},
		},
	}
}

func TestGetPullSecretEmailAction_Name(t *testing.T) {
	action := NewGetPullSecretEmailAction()
	if action.Name() != "get-pull-secret-email" {
		t.Errorf("expected name %q, got %q", "get-pull-secret-email", action.Name())
	}
}

func TestGetPullSecretEmailAction_RequiredRBAC(t *testing.T) {
	action := NewGetPullSecretEmailAction()
	rules := action.RequiredRBAC(ResourceTarget{})

	if len(rules) != 1 {
		t.Fatalf("expected 1 RBAC rule, got %d", len(rules))
	}

	rule := rules[0]
	if rule.APIGroups[0] != "" {
		t.Errorf("expected empty API group, got %q", rule.APIGroups[0])
	}
	if rule.Resources[0] != "secrets" {
		t.Errorf("expected resource %q, got %q", "secrets", rule.Resources[0])
	}
	if rule.Verbs[0] != "get" {
		t.Errorf("expected verb %q, got %q", "get", rule.Verbs[0])
	}
	if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "pull-secret" {
		t.Errorf("expected ResourceNames [pull-secret], got %v", rule.ResourceNames)
	}
}

func TestGetPullSecretEmailAction_Execute_Success(t *testing.T) {
	client := newPullSecretFakeClient(newPullSecret("user@example.com"))
	action := NewGetPullSecretEmailAction()

	result, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}

	email, ok := result.Resources[0].Object["email"].(string)
	if !ok {
		t.Fatal("expected email to be a string")
	}
	if email != "user@example.com" {
		t.Errorf("expected email %q, got %q", "user@example.com", email)
	}

	if result.Message != "retrieved pull secret email" {
		t.Errorf("expected message %q, got %q", "retrieved pull secret email", result.Message)
	}
}

func TestGetPullSecretEmailAction_Execute_SecretNotFound(t *testing.T) {
	client := newPullSecretFakeClient()
	action := NewGetPullSecretEmailAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for missing secret, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_NoDockerConfigJSON(t *testing.T) {
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				"other-key": "value",
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for missing .dockerconfigjson, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_NoCloudOpenShiftEntry(t *testing.T) {
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"registry.redhat.io": map[string]interface{}{
				"auth":  "dGVzdDp0ZXN0",
				"email": "user@example.com",
			},
		},
	}
	raw, err := json.Marshal(dockerConfig)
	if err != nil {
		t.Fatalf("failed to marshal dockerConfig fixture: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				".dockerconfigjson": encoded,
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err = action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for missing cloud.openshift.com entry, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_EmptyEmail(t *testing.T) {
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"cloud.openshift.com": map[string]interface{}{
				"auth":  "dGVzdDp0ZXN0",
				"email": "",
			},
		},
	}
	raw, err := json.Marshal(dockerConfig)
	if err != nil {
		t.Fatalf("failed to marshal dockerConfig fixture: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				".dockerconfigjson": encoded,
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err = action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for empty email, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_InvalidBase64(t *testing.T) {
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				".dockerconfigjson": "not-valid-base64!@#$",
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_InvalidJSON(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("not json"))

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
			"data": map[string]interface{}{
				".dockerconfigjson": encoded,
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestGetPullSecretEmailAction_Execute_NoDataField(t *testing.T) {
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "pull-secret",
				"namespace": "openshift-config",
			},
		},
	}
	client := newPullSecretFakeClient(secret)
	action := NewGetPullSecretEmailAction()

	_, err := action.Execute(context.Background(), Clients{Dynamic: client}, ActionRequest{})
	if err == nil {
		t.Fatal("expected error for missing data field, got nil")
	}
}
