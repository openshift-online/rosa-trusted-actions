package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestMockAuthzMiddleware_GrantsSREPRole(t *testing.T) {
	m := NewMockAuthzMiddleware(logrus.New())

	var gotRole string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRole = GetRoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Authn middleware must run before authz; inject a dev identity manually.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := SetCallerIdentityContext(req.Context(), &CallerIdentity{
		Username: "dev-user",
		Email:    "dev-user@redhat.com",
	})

	rr := httptest.NewRecorder()
	m.AuthorizeAPI(next).ServeHTTP(rr, req.WithContext(ctx))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotRole != "SREP" {
		t.Errorf("expected role 'SREP', got %q", gotRole)
	}
}

// TestMockAuthzMiddleware_MockPlusMockAuthn verifies the two mock middlewares
// compose correctly, which mirrors the wiring in cmd/server/main.go.
func TestMockAuthzMiddleware_MockPlusMockAuthn(t *testing.T) {
	authn := NewMockAuthMiddleware()
	authz := NewMockAuthzMiddleware(logrus.New())

	var gotUsername, gotRole string
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := GetCallerIdentityFromContext(r.Context()); id != nil {
			gotUsername = id.Username
		}
		gotRole = GetRoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	chain := authn.AuthenticateAccountJWT(authz.AuthorizeAPI(leaf))
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotUsername != "dev-user" {
		t.Errorf("expected username 'dev-user', got %q", gotUsername)
	}
	if gotRole != "SREP" {
		t.Errorf("expected role 'SREP', got %q", gotRole)
	}
}

func TestMockAuthzMiddleware_SatisfiesInterface(t *testing.T) {
	var _ AuthorizationMiddleware = NewMockAuthzMiddleware(logrus.New())
}
