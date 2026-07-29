package auth

import (
	"net/http"
)

// MiddlewareMock passes all requests through with a default test identity.
// Adapted from rh-trex/pkg/auth/auth_middleware_mock.go.
type MiddlewareMock struct{}

var _ JWTMiddleware = &MiddlewareMock{}

func (a *MiddlewareMock) AuthenticateAccountJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := SetCallerIdentityContext(r.Context(), &CallerIdentity{
			Username: "test-user",
			Email:    "test-user@redhat.com",
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
