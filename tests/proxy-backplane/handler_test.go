package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

func newTestRouter() *chi.Mux {
	store := NewActionStore()
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	h := &Handler{
		store:      store,
		logger:     logger,
		listenAddr: ":9090",
	}

	r := chi.NewRouter()
	r.Post("/backplane/trustedactions/{cluster_id}", h.Register)
	r.Get("/backplane/trustedactions/{cluster_id}/{instanceId}", h.Status)
	r.Delete("/backplane/trustedactions/{cluster_id}/{instanceId}", h.Delete)

	return r
}

func makeKubeconfig(serverURL, token string) string {
	yaml := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: %s
`, serverURL, token)
	return base64.StdEncoding.EncodeToString([]byte(yaml))
}

func doRequest(t *testing.T, method, url string, body string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, url, reader)
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, url, nil)
	}
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	return resp
}

func registerAction(t *testing.T, ts *httptest.Server, clusterID, kubeconfig string) (int, registerResponse) {
	t.Helper()
	body := fmt.Sprintf(`{"name":"get","customerDataAccess":"view","kubeconfig":%q}`, kubeconfig)
	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedactions/"+clusterID, body)
	defer resp.Body.Close()

	var result registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil && resp.StatusCode == http.StatusOK {
		t.Fatalf("failed to decode register response: %v", err)
	}
	return resp.StatusCode, result
}

func TestRegister_Success(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	kc := makeKubeconfig("https://api.cluster.example.com:6443", "my-token")
	status, result := registerAction(t, ts, "cluster-abc", kc)

	if status != http.StatusOK {
		t.Fatalf("got status %d, want %d", status, http.StatusOK)
	}
	if result.InstanceID == "" {
		t.Fatal("expected non-empty instanceId")
	}
	if !strings.HasPrefix(result.InstanceID, "get--") {
		t.Errorf("instanceId %q should start with %q", result.InstanceID, "get--")
	}
	if result.ProxyURI == "" {
		t.Fatal("expected non-empty proxyUri")
	}
	if !strings.Contains(result.ProxyURI, "/backplane/trustedaction/cluster-abc/") {
		t.Errorf("proxyUri %q should contain cluster path", result.ProxyURI)
	}
}

func TestRegister_MissingKubeconfig(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedactions/cluster-1", `{"name":"get","customerDataAccess":"view"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var errResp jsonError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if !strings.Contains(errResp.Message, "kubeconfig field is required") {
		t.Errorf("got message %q, want it to mention kubeconfig required", errResp.Message)
	}
}

func TestRegister_InvalidBase64(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedactions/cluster-1", `{"name":"get","kubeconfig":"not-valid-base64!!!"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var errResp jsonError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if !strings.Contains(errResp.Message, "not valid base64") {
		t.Errorf("got message %q, want it to mention base64", errResp.Message)
	}
}

func TestRegister_MissingServer(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	yaml := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ""
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: some-token
`
	kc := base64.StdEncoding.EncodeToString([]byte(yaml))
	status, _ := registerAction(t, ts, "cluster-1", kc)

	if status != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", status, http.StatusBadRequest)
	}
}

func TestRegister_MissingCredentials(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	yaml := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://api.cluster.example.com:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user: {}
`
	kc := base64.StdEncoding.EncodeToString([]byte(yaml))
	status, _ := registerAction(t, ts, "cluster-1", kc)

	if status != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", status, http.StatusBadRequest)
	}
}

func TestRegister_InvalidJSON(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedactions/cluster-1", "{bad")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestStatus_Found(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	kc := makeKubeconfig("https://api.cluster.example.com:6443", "my-token")
	_, reg := registerAction(t, ts, "cluster-abc", kc)

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedactions/cluster-abc/"+reg.InstanceID, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if result.InstanceID != reg.InstanceID {
		t.Errorf("got instanceId %q, want %q", result.InstanceID, reg.InstanceID)
	}
	if result.ProxyURI == "" {
		t.Error("expected non-empty proxyUri")
	}
}

func TestStatus_NotFound(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedactions/cluster-abc/no-such-instance", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var errResp jsonError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.StatusCode != http.StatusNotFound {
		t.Errorf("got body statusCode %d, want %d", errResp.StatusCode, http.StatusNotFound)
	}
}

func TestDelete_Success(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	kc := makeKubeconfig("https://api.cluster.example.com:6443", "my-token")
	_, reg := registerAction(t, ts, "cluster-abc", kc)

	resp := doRequest(t, http.MethodDelete, ts.URL+"/backplane/trustedactions/cluster-abc/"+reg.InstanceID, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	getResp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedactions/cluster-abc/"+reg.InstanceID, "")
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestDelete_NotFound(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodDelete, ts.URL+"/backplane/trustedactions/cluster-abc/no-such-instance", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestCrossClusterIsolation_HTTP(t *testing.T) {
	router := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	kc := makeKubeconfig("https://api.cluster-a.example.com:6443", "token-a")
	_, reg := registerAction(t, ts, "cluster-a", kc)

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedactions/cluster-b/"+reg.InstanceID, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-cluster access should return 404, got %d", resp.StatusCode)
	}

	resp2 := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedactions/cluster-a/"+reg.InstanceID, "")
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("same-cluster access should return 200, got %d", resp2.StatusCode)
	}
}
