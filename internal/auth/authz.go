package auth

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/ocm"
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

// ActionCatalog provides action lookup for authorization checks
type ActionCatalog interface {
	GetAction(name string) (*Action, bool)
}

// Action represents a trusted action with its authorization requirements
type Action struct {
	Name         string
	Description  string
	AllowedRoles []string
}

// ActionAuthzMiddleware checks that the caller's resolved role is in the
// action's allowedRoles list. Enforced on any request with an {action} URL param.
type ActionAuthzMiddleware struct {
	catalog ActionCatalog
	logger  *logrus.Logger
}

func NewActionAuthzMiddleware(catalog ActionCatalog, logger *logrus.Logger) *ActionAuthzMiddleware {
	return &ActionAuthzMiddleware{
		catalog: catalog,
		logger:  logger,
	}
}

// CheckActionAccess is a chi MiddlewareFunc that enforces per-action role requirements.
// Requests without an {action} URL param pass through (protected by upstream authn/authz).
func (m *ActionAuthzMiddleware) CheckActionAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actionName := chi.URLParam(r, "action")
		if actionName == "" {
			next.ServeHTTP(w, r)
			return
		}

		action, ok := m.catalog.GetAction(actionName)
		if !ok {
			respondError(w, http.StatusNotFound, "Unknown action")
			return
		}

		role := GetRoleFromContext(r.Context())
		if role == "" {
			respondError(w, http.StatusForbidden, "Access denied")
			return
		}

		if !slices.Contains(action.AllowedRoles, role) {
			m.logger.WithFields(logrus.Fields{
				"action": actionName,
				"role":   role,
			}).Warn("Action not authorized for role")
			respondError(w, http.StatusForbidden, "Access denied")
			return
		}

		next.ServeHTTP(w, r)
	})
}
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
			m.logger.Warn("Authentication details not present in context")
			respondError(w, http.StatusUnauthorized, "Authentication required.")
			return
		}

		var hadErr bool

		for _, role := range m.roles {
			allowed, err := m.authz.AccessReview(
				ctx, identity.Username, "*", role.AMSResource)
			if err != nil {
				hadErr = true
				m.logger.WithError(err).WithFields(logrus.Fields{
					"username": identity.Username,
					"role":     role.ID,
					"resource": role.AMSResource,
				}).Error("AMS AccessReview failed")
				continue
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

		if hadErr {
			respondError(w, http.StatusInternalServerError, "Authorization check failed")
			return
		}

		m.logger.WithField("username", identity.Username).Warn("No matching role found")
		respondError(w, http.StatusForbidden, "Access denied.")
	})
}
