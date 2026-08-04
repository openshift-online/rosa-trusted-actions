package auth

import (
	"net/http"

	"github.com/sirupsen/logrus"
)

// MockAuthzMiddleware is used for local development only.
// It grants the highest-privilege role (SREP) unconditionally so that all
// action-level checks pass without an AMS AccessReview call.
//
// WARNING: never enable in production. Use ROSA_TA_ENABLE_AUTH=false only in
// local or CI environments where real OCM credentials are unavailable.
type MockAuthzMiddleware struct{ logger *logrus.Logger }

var _ AuthorizationMiddleware = &MockAuthzMiddleware{}

func NewMockAuthzMiddleware(logger *logrus.Logger) *MockAuthzMiddleware {
	return &MockAuthzMiddleware{logger: logger}
}

func (m *MockAuthzMiddleware) AuthorizeAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.logger.Warn("Mock authz active — granting SREP role without AMS check. Do not use in production.")
		ctx := SetRoleInContext(r.Context(), "SREP")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
