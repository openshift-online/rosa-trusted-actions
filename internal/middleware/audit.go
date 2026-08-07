package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
)

type auditContextKey struct{}

// AuditContext holds fields that handlers can attach to enrich the audit entry.
type AuditContext struct {
	ExecutionID   string
	Jira          string
	ApprovalState string
	TargetCluster string
}

// SetAuditContext stores handler-provided audit fields in the request context.
func SetAuditContext(ctx context.Context, ac *AuditContext) context.Context {
	return context.WithValue(ctx, auditContextKey{}, ac)
}

// GetAuditContext retrieves handler-provided audit fields from the request context.
func GetAuditContext(ctx context.Context) *AuditContext {
	val, _ := ctx.Value(auditContextKey{}).(*AuditContext)
	return val
}

// NewAuditLogger creates middleware that records an audit entry for every API request.
func NewAuditLogger(s store.Store, logger *logrus.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac := &AuditContext{}
			ctx := SetAuditContext(r.Context(), ac)
			r = r.WithContext(ctx)

			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			username := ""
			if identity := auth.GetCallerIdentityFromContext(r.Context()); identity != nil {
				username = identity.Username
			}

			action := chi.URLParam(r, "action")

			entry := &models.AuditEntry{
				ID:         uuid.New(),
				Timestamp:  time.Now().UTC(),
				Method:     r.Method,
				Path:       r.URL.Path,
				Username:   username,
				StatusCode: ww.Status(),
			}

			if action != "" {
				entry.Action = &action
			}
			if ac.ExecutionID != "" {
				entry.ExecutionID = &ac.ExecutionID
			}
			if ac.Jira != "" {
				entry.Jira = &ac.Jira
			}
			if ac.ApprovalState != "" {
				entry.ApprovalState = &ac.ApprovalState
			}
			if ac.TargetCluster != "" {
				entry.TargetCluster = &ac.TargetCluster
			}

			// Detached context: r.Context() may already be cancelled after next.ServeHTTP returns.
			writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.CreateAuditEntry(writeCtx, entry); err != nil { //nolint:contextcheck
				logger.WithError(err).Error("Failed to write audit entry")
			}
		})
	}
}
