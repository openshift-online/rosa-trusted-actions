package auth

import (
	"net/http"

	"github.com/sirupsen/logrus"
)

// AuthzMiddlewareMock passes all requests through with a default role.
// Adapted from rh-trex/pkg/auth/authz_middleware_mock.go.
type AuthzMiddlewareMock struct {
	logger *logrus.Logger
}

var _ AuthorizationMiddleware = &AuthzMiddlewareMock{}

func NewAuthzMiddlewareMock(logger *logrus.Logger) *AuthzMiddlewareMock {
	return &AuthzMiddlewareMock{logger: logger}
}

func (m *AuthzMiddlewareMock) AuthorizeAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Header.Get("X-Mock-Role")
		if role == "" {
			respondError(w, http.StatusForbidden, "Missing X-Mock-Role header (mock authz mode)")
			return
		}
		m.logger.WithField("role", role).Debug("Mock authz: allowing request")
		ctx := SetRoleInContext(r.Context(), role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
