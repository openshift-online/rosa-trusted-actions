package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/openapi"
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
			respondError(w, http.StatusUnauthorized, "Authentication required.")
			return
		}

		if identity.Username == "" {
			a.logger.WithError(err).Warn("JWT token missing username claim")
			respondError(w, http.StatusUnauthorized, "Authentication required.")
			return
		}

		ctx := SetCallerIdentityContext(r.Context(), identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func respondError(w http.ResponseWriter, status int, message string) {
	errorResp := openapi.Error{
		Kind:   openapi.ErrorKindError,
		Code:   fmt.Sprintf("HTTP_%d", status),
		Reason: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	json.NewEncoder(w).Encode(errorResp)
}
