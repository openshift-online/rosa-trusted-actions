package auth

import "net/http"

// MockMiddleware is used for local development only.
// It injects a hardcoded identity into every request context — it reads
// nothing from request headers and cannot be used to impersonate another user.
//
// WARNING: never enable in production. Use ROSA_TA_ENABLE_AUTH=false only in
// local or CI environments where real OCM credentials are unavailable.
type MockMiddleware struct{}

var _ JWTMiddleware = &MockMiddleware{}

func NewMockAuthMiddleware() *MockMiddleware { return &MockMiddleware{} }

func (m *MockMiddleware) AuthenticateAccountJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := SetCallerIdentityContext(r.Context(), &CallerIdentity{
			Username: "dev-user",
			Email:    "dev-user@redhat.com",
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
