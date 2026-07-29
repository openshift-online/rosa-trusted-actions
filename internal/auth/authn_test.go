package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMockMiddleware_WithUsername(t *testing.T) {
	middleware := NewMockAuthMiddleware(nil)

	var capturedIdentity *CallerIdentity
	handler := middleware.AuthenticateAccountJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIdentity = GetCallerIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Mock-Username", "srep-user")
	req.Header.Set("X-Mock-Email", "srep@redhat.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedIdentity == nil {
		t.Fatal("expected identity in context")
	}
	if capturedIdentity.Username != "srep-user" {
		t.Errorf("expected username 'srep-user', got %q", capturedIdentity.Username)
	}
	if capturedIdentity.Email != "srep@redhat.com" {
		t.Errorf("expected email 'srep@redhat.com', got %q", capturedIdentity.Email)
	}
}

func TestMockMiddleware_MissingUsername_Returns401(t *testing.T) {
	middleware := NewMockAuthMiddleware(nil)

	handler := middleware.AuthenticateAccountJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestMiddlewareMock_SetsDefaultIdentity(t *testing.T) {
	mock := &MiddlewareMock{}

	var capturedIdentity *CallerIdentity
	handler := mock.AuthenticateAccountJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIdentity = GetCallerIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedIdentity == nil {
		t.Fatal("expected identity in context")
	}
	if capturedIdentity.Username != "test-user" {
		t.Errorf("expected username 'test-user', got %q", capturedIdentity.Username)
	}
}
