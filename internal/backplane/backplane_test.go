package backplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestKubeconfigProvider_InvalidPath(t *testing.T) {
	provider := NewKubeconfigProvider(logrus.New(), "/nonexistent/kubeconfig")

	_, err := provider.GetClient(context.Background(), "cluster-123", nil)
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path, got nil")
	}
}

func TestBackplaneProvider_RequestAccess(t *testing.T) {
	expectedInstanceID := "trusted_actions--test-uuid-1234"
	clientID := "trusted-actions"
	clientSecret := "test-secret"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("X-Caller-ID") != clientID {
			t.Errorf("expected X-Caller-ID %q, got %q", clientID, r.Header.Get("X-Caller-ID"))
		}

		if r.Header.Get("X-Signature") == "" {
			t.Error("expected X-Signature header, got empty")
		}

		if r.Header.Get("X-Timestamp") == "" {
			t.Error("expected X-Timestamp header, got empty")
		}

		var req trustedActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if len(req.RBACRules) != 1 {
			t.Errorf("expected 1 RBAC rule, got %d", len(req.RBACRules))
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(trustedActionResponse{InstanceID: expectedInstanceID}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewBackplaneProvider(logrus.New(), server.URL, clientID, clientSecret)

	rules := []RBACRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get"},
	}}

	instanceID, err := provider.requestAccess(context.Background(), "cluster-123", rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instanceID != expectedInstanceID {
		t.Errorf("expected instance ID %q, got %q", expectedInstanceID, instanceID)
	}
}

func TestBackplaneProvider_RequestAccessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": "access denied"}`))
	}))
	defer server.Close()

	provider := NewBackplaneProvider(logrus.New(), server.URL, "test-client", "test-secret")

	_, err := provider.requestAccess(context.Background(), "cluster-123", nil)
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestSignRequest_DoesNotMutateBackingArray(t *testing.T) {
	raw := []byte(`{"data":"value"}`)
	body := make([]byte, len(raw), len(raw)+200)
	copy(body, raw)

	// Snapshot the backing array beyond len(body) — should stay zeroed
	full := body[:cap(body)]
	before := make([]byte, cap(body))
	copy(before, full)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	signRequest(req, body, "client-id", "secret")

	for i := len(raw); i < cap(body); i++ {
		if full[i] != before[i] {
			t.Fatalf("signRequest wrote into body's backing array at offset %d", i)
		}
	}
}

func TestSignRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	body := []byte(`{"test": true}`)
	clientID := "test-caller"
	secret := "test-secret"

	signRequest(req, body, clientID, secret)

	if req.Header.Get("X-Caller-ID") != clientID {
		t.Errorf("expected X-Caller-ID %q, got %q", clientID, req.Header.Get("X-Caller-ID"))
	}

	timestamp := req.Header.Get("X-Timestamp")
	if timestamp == "" {
		t.Fatal("expected X-Timestamp header, got empty")
	}

	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		t.Errorf("X-Timestamp is not valid RFC3339: %v", err)
	}

	signature := req.Header.Get("X-Signature")
	if signature == "" {
		t.Fatal("expected X-Signature header, got empty")
	}

	payload := append(body, []byte(timestamp)...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if signature != expectedSig {
		t.Errorf("signature mismatch: expected %q, got %q", expectedSig, signature)
	}
}
