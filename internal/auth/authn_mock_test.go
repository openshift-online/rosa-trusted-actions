package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMockAuthMiddleware_InjectsHardcodedDevUser(t *testing.T) {
	m := NewMockAuthMiddleware()

	var got *CallerIdentity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = GetCallerIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	m.AuthenticateAccountJWT(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got == nil {
		t.Fatal("expected identity in context, got nil")
	}
	if got.Username != "dev-user" {
		t.Errorf("expected username 'dev-user', got %q", got.Username)
	}
	if got.Email != "dev-user@redhat.com" {
		t.Errorf("expected email 'dev-user@redhat.com', got %q", got.Email)
	}
}

// TestMockAuthMiddleware_IgnoresAllRequestHeaders confirms the security property
// that the mock cannot be used to inject an arbitrary identity via request headers.
func TestMockAuthMiddleware_IgnoresAllRequestHeaders(t *testing.T) {
	m := NewMockAuthMiddleware()

	var got *CallerIdentity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = GetCallerIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Username", "attacker")
	req.Header.Set("Authorization", "Bearer some-forged-token")

	httptest.NewRecorder() // discard
	rr := httptest.NewRecorder()
	m.AuthenticateAccountJWT(next).ServeHTTP(rr, req)

	if got == nil || got.Username != "dev-user" {
		t.Errorf("mock must ignore request headers; got username %q", got.Username)
	}
}

func TestMockAuthMiddleware_SatisfiesInterface(t *testing.T) {
	var _ JWTMiddleware = NewMockAuthMiddleware()
}
