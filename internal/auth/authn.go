package auth

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

// JWTMiddleware defines the authentication middleware interface
type JWTMiddleware interface {
	AuthenticateAccountJWT(next http.Handler) http.Handler
}

// Middleware validates JWT tokens and extracts caller identity.
// Adapted from rh-trex/pkg/auth/auth_middleware.go.
type Middleware struct {
	logger *logrus.Logger
}

var _ JWTMiddleware = &Middleware{}

func NewAuthMiddleware(logger *logrus.Logger) *Middleware {
	return &Middleware{logger: logger}
}

func (a *Middleware) AuthenticateAccountJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := GetCallerIdentityFromJWT(r)
		if err != nil {
			a.logger.WithError(err).Warn("JWT authentication failed")
			respondError(w, http.StatusUnauthorized, "Authentication failed: "+err.Error())
			return
		}

		if identity.Username == "" {
			respondError(w, http.StatusUnauthorized, "JWT token missing username claim")
			return
		}

		ctx := SetCallerIdentityContext(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MockMiddleware reads identity from custom headers for local development.
// Used when EnableAuth=false.
type MockMiddleware struct {
	logger *logrus.Logger
}

var _ JWTMiddleware = &MockMiddleware{}

func NewMockAuthMiddleware(logger *logrus.Logger) *MockMiddleware {
	return &MockMiddleware{logger: logger}
}

func (m *MockMiddleware) AuthenticateAccountJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := r.Header.Get("X-Mock-Username")
		if username == "" {
			respondError(w, http.StatusUnauthorized, "Missing X-Mock-Username header (mock auth mode)")
			return
		}

		identity := &CallerIdentity{
			Username: username,
			Email:    r.Header.Get("X-Mock-Email"),
			ClientID: r.Header.Get("X-Mock-ClientID"),
		}

		if m.logger != nil {
			m.logger.WithField("username", username).Debug("Mock authentication")
		}

		ctx := SetCallerIdentityContext(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"kind":   "Error",
		"reason": message,
	})
}
