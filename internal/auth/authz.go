package auth

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/ocm"
)

// AuthorizationMiddleware defines the role-resolution middleware interface
type AuthorizationMiddleware interface {
	AuthorizeAPI(next http.Handler) http.Handler
}

// RoleAuthzMiddleware iterates configured role mappings and resolves the caller's
// role via AMS AccessReview. Substantially reworked from rh-trex's single-check pattern
// to support the backplane-api role-iteration model.
type RoleAuthzMiddleware struct {
	roles  []RoleMapping
	authz  ocm.Authorization
	logger *logrus.Logger
}

var _ AuthorizationMiddleware = &RoleAuthzMiddleware{}

func NewRoleAuthzMiddleware(roles []RoleMapping, authz ocm.Authorization, logger *logrus.Logger) *RoleAuthzMiddleware {
	return &RoleAuthzMiddleware{
		roles:  roles,
		authz:  authz,
		logger: logger,
	}
}

func (m *RoleAuthzMiddleware) AuthorizeAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		identity := GetCallerIdentityFromContext(ctx)
		if identity == nil || identity.Username == "" {
			respondError(w, http.StatusUnauthorized, "Authentication details not present in context")
			return
		}

		for _, role := range m.roles {
			allowed, err := m.authz.AccessReview(
				ctx, identity.Username, "*", role.AMSResource, "", "", "")
			if err != nil {
				m.logger.WithError(err).WithFields(logrus.Fields{
					"username": identity.Username,
					"role":     role.ID,
					"resource": role.AMSResource,
				}).Error("AMS AccessReview failed")
				respondError(w, http.StatusInternalServerError, "Authorization check failed")
				return
			}

			if allowed {
				m.logger.WithFields(logrus.Fields{
					"username": identity.Username,
					"role":     role.ID,
				}).Debug("Role resolved via AMS")

				ctx = SetRoleInContext(ctx, role.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		m.logger.WithField("username", identity.Username).Warn("No matching role found")
		respondError(w, http.StatusForbidden, "Access denied: no authorized role for this user")
	})
}
