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
		m.logger.Debug("Mock authz: allowing request with default role SREP")
		ctx := SetRoleInContext(r.Context(), "SREP")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
