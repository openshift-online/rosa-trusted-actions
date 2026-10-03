package backplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestKubeconfigProvider_InvalidPath(t *testing.T) {
	provider := NewKubeconfigProvider(logrus.New(), "/nonexistent/kubeconfig")

	_, err := provider.GetClient(context.Background(), "cluster-123", "get", nil)
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path, got nil")
	}
}

func TestBackplaneProvider_RequestAccess(t *testing.T) {
	expectedInstanceID := "get--test-uuid-1234"
	expectedProxyUri := "/backplane/trustedaction/cluster-123/" + expectedInstanceID + "/"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/backplane/trustedactions/cluster-123" {
			t.Errorf("expected path /backplane/trustedactions/cluster-123, got %s", r.URL.Path)
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			t.Errorf("expected Authorization 'Bearer test-token', got %q", authHeader)
		}

		var req trustedActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if req.Name != "get" {
			t.Errorf("expected action name 'get', got %q", req.Name)
		}

		if len(req.Rbac.Roles) != 1 {
			t.Fatalf("expected 1 namespace role, got %d", len(req.Rbac.Roles))
		}
		if req.Rbac.Roles[0].Namespace != "openshift-monitoring" {
			t.Errorf("expected namespace 'openshift-monitoring', got %q", req.Rbac.Roles[0].Namespace)
		}
		if len(req.Rbac.Roles[0].Rules) != 1 {
			t.Errorf("expected 1 rule in namespace role, got %d", len(req.Rbac.Roles[0].Rules))
		}

		if len(req.Rbac.ClusterRoleRules) != 0 {
			t.Errorf("expected 0 cluster role rules, got %d", len(req.Rbac.ClusterRoleRules))
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(trustedActionResponse{
			ProxyUri:   expectedProxyUri,
			InstanceId: expectedInstanceID,
			Expiry:     "2026-09-04T12:00:00Z",
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	tokenFunc := func(_ context.Context) (string, error) { return "test-token", nil }
	provider := NewBackplaneProvider(logrus.New(), server.URL, tokenFunc)

	rules := []RBACRule{{
		Namespace: "openshift-monitoring",
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get"},
	}}

	resp, err := provider.requestAccess(context.Background(), "cluster-123", "get", rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.InstanceId != expectedInstanceID {
		t.Errorf("expected instance ID %q, got %q", expectedInstanceID, resp.InstanceId)
	}
	if resp.ProxyUri != expectedProxyUri {
		t.Errorf("expected proxy URI %q, got %q", expectedProxyUri, resp.ProxyUri)
	}
}

func TestBackplaneProvider_RequestAccessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": "access denied"}`))
	}))
	defer server.Close()

	tokenFunc := func(_ context.Context) (string, error) { return "test-token", nil }
	provider := NewBackplaneProvider(logrus.New(), server.URL, tokenFunc)

	_, err := provider.requestAccess(context.Background(), "cluster-123", "get", nil)
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestBuildTrustedActionRequest_SplitsRules(t *testing.T) {
	rules := []RBACRule{
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"list"}},
		{Namespace: "openshift-monitoring", APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		{Namespace: "openshift-monitoring", APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
		{Namespace: "openshift-config", APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
	}

	req := buildTrustedActionRequest("test-action", rules)

	if req.Name != "test-action" {
		t.Errorf("expected name 'test-action', got %q", req.Name)
	}

	if len(req.Rbac.ClusterRoleRules) != 1 {
		t.Fatalf("expected 1 cluster role rule, got %d", len(req.Rbac.ClusterRoleRules))
	}
	if req.Rbac.ClusterRoleRules[0].Resources[0] != "nodes" {
		t.Errorf("expected cluster role rule for 'nodes', got %q", req.Rbac.ClusterRoleRules[0].Resources[0])
	}

	if len(req.Rbac.Roles) != 2 {
		t.Fatalf("expected 2 namespace roles, got %d", len(req.Rbac.Roles))
	}

	rolesByNS := make(map[string]int)
	for _, role := range req.Rbac.Roles {
		rolesByNS[role.Namespace] = len(role.Rules)
	}
	if rolesByNS["openshift-monitoring"] != 2 {
		t.Errorf("expected 2 rules for openshift-monitoring, got %d", rolesByNS["openshift-monitoring"])
	}
	if rolesByNS["openshift-config"] != 1 {
		t.Errorf("expected 1 rule for openshift-config, got %d", rolesByNS["openshift-config"])
	}
}
