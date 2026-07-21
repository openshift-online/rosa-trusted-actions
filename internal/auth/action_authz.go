package auth

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

// ActionCatalog provides action lookup for authorization checks
type ActionCatalog interface {
	GetAction(name string) (*Action, bool)
}

// Action represents a trusted action with its authorization requirements
type Action struct {
	Name          string
	Description   string
	RequiredRoles []string
}

// ActionAuthzMiddleware checks that the caller's resolved role is in the
// action's requiredRoles list. Enforced on any request with an {action} URL param.
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

		if !slices.Contains(action.RequiredRoles, role) {
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
