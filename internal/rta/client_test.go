package rta

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func TestNewClient_MissingServer(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("expected error for empty server URL")
	}
	if got := err.Error(); got != "server URL is required; set --server or RTA_SERVER" {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestNewClient_TrailingSlash(t *testing.T) {
	c, err := NewClient("http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if c.base != "http://example.com"+basePath {
		t.Errorf("base = %q, want no trailing slash before basePath", c.base)
	}
}

func TestNewClient_WithToken(t *testing.T) {
	c, err := NewClient("http://localhost:8080", WithToken("tok123"))
	if err != nil {
		t.Fatal(err)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q, want tok123", c.token)
	}
}

func TestNewClient_RejectsTokenOverHTTP(t *testing.T) {
	_, err := NewClient("http://example.com", WithToken("tok123"))
	if err == nil {
		t.Fatal("expected error for token over plain HTTP to non-loopback host")
	}
	if !strings.Contains(err.Error(), "refusing to send credentials") {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestNewClient_AllowsTokenOverHTTPS(t *testing.T) {
	c, err := NewClient("https://example.com", WithToken("tok123"))
	if err != nil {
		t.Fatal(err)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q", c.token)
	}
}

func TestClient_Catalog(t *testing.T) {
	catalog := openapi.TrustedActionCatalog{
		Total: 1,
		Items: []openapi.TrustedActionSummary{
			{Name: "get", Scope: "kube-api", Type: "read", Description: "Get resources"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != basePath+"/" {
			t.Errorf("path = %q, want %q", r.URL.Path, basePath+"/")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("missing Accept header")
		}
		_ = json.NewEncoder(w).Encode(catalog)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	got, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].Name != "get" {
		t.Errorf("unexpected catalog: %+v", got)
	}
}

func TestClient_Describe(t *testing.T) {
	action := openapi.TrustedAction{
		Name: "get", Scope: "kube-api", Type: "read", Description: "Get resources",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != basePath+"/get" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(action)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	got, err := c.Describe(context.Background(), "get")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "get" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestClient_CreateExecution(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	exec := openapi.Execution{Id: id, Action: "get", TargetCluster: "c1", Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != basePath+"/get/run" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		var req openapi.ExecutionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TargetCluster != "c1" {
			t.Errorf("target = %q", req.TargetCluster)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(exec)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, WithToken("test-token"))
	got, err := c.CreateExecution(context.Background(), "get", openapi.ExecutionRequest{TargetCluster: "c1", Jira: "OHSS-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != id {
		t.Errorf("id = %s", got.Id)
	}
}

func TestClient_ListExecutions_WithQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != basePath+"/runs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("status") != "running" {
			t.Errorf("query status = %q", r.URL.Query().Get("status"))
		}
		_ = json.NewEncoder(w).Encode(openapi.ExecutionList{Items: []openapi.Execution{}})
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	q := url.Values{"status": []string{"running"}}
	got, err := c.ListExecutions(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 {
		t.Errorf("expected empty list")
	}
}

func TestClient_APIError_Structured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(openapi.Error{Kind: "Error", Code: "invalid-params", Reason: "missing resource"})
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	_, err := c.Catalog(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
	if apiErr.Code != "invalid-params" {
		t.Errorf("code = %q", apiErr.Code)
	}
	if apiErr.Reason != "missing resource" {
		t.Errorf("reason = %q", apiErr.Reason)
	}
}

func TestClient_APIError_Unstructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	_, err := c.Catalog(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
	if apiErr.Code != "" {
		t.Errorf("expected empty code for unstructured error, got %q", apiErr.Code)
	}
}

func TestClient_AuthHeader_Present(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(openapi.TrustedActionCatalog{})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, WithToken("my-token"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClient_AuthHeader_Absent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no auth header, got %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(openapi.TrustedActionCatalog{})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClient_UserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("missing User-Agent")
		}
		if !strings.Contains(ua, "rta/") {
			t.Errorf("User-Agent %q missing rta/ prefix", ua)
		}
		_ = json.NewEncoder(w).Encode(openapi.TrustedActionCatalog{})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatal(err)
	}
}
