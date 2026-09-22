package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupProxyTest(t *testing.T, upstreamHandler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()

	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)

	router, store := newTestRouter()
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	instanceID := "test-instance-id"
	store.Put(&ActionEntry{
		ClusterID:  "cluster-1",
		InstanceID: instanceID,
		Host:       upstream.URL,
		Transport:  &http.Transport{},
		Token:      "stored-token",
	})

	return ts, instanceID
}

func TestHandler_Proxy_GET(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("X-Upstream") != "true" {
		t.Error("expected upstream response header to pass through")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"result":"ok"}` {
		t.Errorf("got body %q, want %q", string(body), `{"result":"ok"}`)
	}
}

func TestHandler_Proxy_POST(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("upstream got method %q, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		w.Write(body)
	})

	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/namespaces", `{"name":"test-ns"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"name":"test-ns"}` {
		t.Errorf("got body %q, want request body forwarded", string(body))
	}
}

func TestHandler_Proxy_PUT(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("upstream got method %q, want PUT", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	resp := doRequest(t, http.MethodPut, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/configmaps/test", `{"data":"updated"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandler_Proxy_PATCH(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("upstream got method %q, want PATCH", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	resp := doRequest(t, http.MethodPatch, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/pods/test", `{"spec":{}}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandler_Proxy_DELETE(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("upstream got method %q, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"deleted"}`))
	})

	resp := doRequest(t, http.MethodDelete, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/pods/test", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandler_Proxy_PathStripping(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Path))
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", "")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "/api/v1/nodes" {
		t.Errorf("upstream received path %q, want %q", string(body), "/api/v1/nodes")
	}
}

func TestHandler_Proxy_AuthHeaderReplacement(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(auth))
	})

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer caller-token-should-be-stripped")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Bearer stored-token" {
		t.Errorf("upstream got auth %q, want %q", string(body), "Bearer stored-token")
	}
}

func TestHandler_Proxy_QueryParameterForwarding(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.RawQuery))
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/pods?labelSelector=app%3Dtest&limit=10", "")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	got := string(body)
	if !strings.Contains(got, "labelSelector=app%3Dtest") || !strings.Contains(got, "limit=10") {
		t.Errorf("upstream got query %q, want labelSelector and limit params", got)
	}
}

func TestHandler_Proxy_Upstream5xxPassthrough(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"upstream internal error"}`))
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d (upstream 5xx should pass through)", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestHandler_Proxy_UpstreamUnreachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	upstreamURL := upstream.URL
	upstream.Close()

	router, store := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	instanceID := "unreachable-instance"
	store.Put(&ActionEntry{
		ClusterID:  "cluster-1",
		InstanceID: instanceID,
		Host:       upstreamURL,
		Transport:  &http.Transport{},
		Token:      "token",
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	var errResp jsonError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp.StatusCode != http.StatusBadGateway {
		t.Errorf("got body statusCode %d, want %d", errResp.StatusCode, http.StatusBadGateway)
	}
	if errResp.Message == "" {
		t.Error("expected non-empty error message with connection details")
	}
}

func TestHandler_Proxy_NotFound(t *testing.T) {
	router, _ := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/no-such-instance/api/v1/nodes", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var errResp jsonError
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp.StatusCode != http.StatusNotFound {
		t.Errorf("got body statusCode %d, want %d", errResp.StatusCode, http.StatusNotFound)
	}
}

func TestHandler_Proxy_ResponseHeadersPassThrough(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("X-Request-Id", "upstream-req-id")
		w.WriteHeader(http.StatusOK)
	})

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/nodes", "")
	defer resp.Body.Close()

	if resp.Header.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header to pass through, got %q", resp.Header.Get("X-Custom-Header"))
	}
	if resp.Header.Get("X-Request-Id") != "upstream-req-id" {
		t.Errorf("expected X-Request-Id to pass through, got %q", resp.Header.Get("X-Request-Id"))
	}
}

func makeInsecureKubeconfig(serverURL, token string) string {
	yaml := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    insecure-skip-tls-verify: true
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

func TestHandler_Proxy_EndToEnd(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]string{
			"path":   r.URL.Path,
			"method": r.Method,
			"auth":   auth,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	router, _ := newTestRouter()
	ts := httptest.NewServer(router)
	defer ts.Close()

	kc := makeInsecureKubeconfig(upstream.URL, "e2e-token")
	status, reg := registerAction(t, ts, "cluster-e2e", kc)
	if status != http.StatusOK {
		t.Fatalf("register failed with status %d", status)
	}

	resp := doRequest(t, http.MethodGet, ts.URL+"/backplane/trustedaction/cluster-e2e/"+reg.InstanceID+"/api/v1/pods", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode upstream response: %v", err)
	}
	if result["path"] != "/api/v1/pods" {
		t.Errorf("upstream got path %q, want /api/v1/pods", result["path"])
	}
	if result["auth"] != "Bearer e2e-token" {
		t.Errorf("upstream got auth %q, want Bearer e2e-token", result["auth"])
	}
}

func TestHandler_Proxy_RequestBodyForwarding(t *testing.T) {
	ts, instanceID := setupProxyTest(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	largeBody := `{"metadata":{"name":"test","namespace":"default"},"spec":{"containers":[{"name":"app","image":"nginx:latest"}]}}`
	resp := doRequest(t, http.MethodPost, ts.URL+"/backplane/trustedaction/cluster-1/"+instanceID+"/api/v1/pods", largeBody)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != largeBody {
		t.Errorf("request body not forwarded correctly")
	}
}
