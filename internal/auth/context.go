package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openshift-online/ocm-sdk-go/authentication"
)

type contextKey string

const (
	contextIdentityKey contextKey = "caller_identity"
	contextRoleKey     contextKey = "caller_role"
)

// CallerIdentity represents the authenticated caller extracted from JWT claims or mock headers
type CallerIdentity struct {
	Username string
	Email    string
	ClientID string
}

func SetCallerIdentityContext(ctx context.Context, identity *CallerIdentity) context.Context {
	return context.WithValue(ctx, contextIdentityKey, identity)
}

func GetCallerIdentityFromContext(ctx context.Context) *CallerIdentity {
	val := ctx.Value(contextIdentityKey)
	if val == nil {
		return nil
	}
	return val.(*CallerIdentity)
}

func SetRoleInContext(ctx context.Context, roleID string) context.Context {
	return context.WithValue(ctx, contextRoleKey, roleID)
}

func GetRoleFromContext(ctx context.Context) string {
	val := ctx.Value(contextRoleKey)
	if val == nil {
		return ""
	}
	return val.(string)
}

// GetCallerIdentityFromJWT extracts caller identity from the OCM JWT token in the request context.
// Adapted from rh-trex/pkg/auth/context.go GetAuthPayloadFromContext.
func GetCallerIdentityFromJWT(r *http.Request) (*CallerIdentity, error) {
	userToken, err := authentication.TokenFromContext(r.Context())
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve JWT token from request context: %v", err)
	}

	if userToken == nil {
		return nil, fmt.Errorf("JWT token in context is nil, unauthorized")
	}

	claims, ok := userToken.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("unable to parse JWT token claims: %#v", userToken.Claims)
	}

	identity := &CallerIdentity{}

	// RHSSO claim keys
	identity.Username, _ = claims["username"].(string)
	identity.Email, _ = claims["email"].(string)
	identity.ClientID, _ = claims["clientId"].(string)

	// Fallback to RHD claim keys
	if identity.Username == "" {
		identity.Username, _ = claims["preferred_username"].(string)
	}

	return identity, nil
}
