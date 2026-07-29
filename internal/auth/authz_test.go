package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/ocm"
)

func TestRoleAuthzMiddleware_GrantsSREP(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	mockAuthz := &ocm.ConfigurableMockAuthorization{
		Permissions: map[string][]string{
			"srep-user": {"BackplaneOsdSrepResource"},
		},
	}

	roles := []RoleMapping{
		{ID: "SREP", AMSResource: "BackplaneOsdSrepResource"},
		{ID: "ConfigurationAnomalyDetection", AMSResource: "BackplaneOsdCadResource"},
	}

	middleware := NewRoleAuthzMiddleware(roles, mockAuthz, logger)

	var capturedRole string
	handler := middleware.AuthorizeAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRole = GetRoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := SetCallerIdentityContext(req.Context(), &CallerIdentity{Username: "srep-user"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedRole != "SREP" {
		t.Errorf("expected role 'SREP', got %q", capturedRole)
	}
}

func TestRoleAuthzMiddleware_GrantsCAD(t *testing.T) {
	logger := logrus.New()

	mockAuthz := &ocm.ConfigurableMockAuthorization{
		Permissions: map[string][]string{
			"cad-service": {"BackplaneOsdCadResource"},
		},
	}

	roles := []RoleMapping{
		{ID: "SREP", AMSResource: "BackplaneOsdSrepResource"},
		{ID: "ConfigurationAnomalyDetection", AMSResource: "BackplaneOsdCadResource"},
	}

	middleware := NewRoleAuthzMiddleware(roles, mockAuthz, logger)

	var capturedRole string
	handler := middleware.AuthorizeAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRole = GetRoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := SetCallerIdentityContext(req.Context(), &CallerIdentity{Username: "cad-service"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedRole != "ConfigurationAnomalyDetection" {
		t.Errorf("expected role 'ConfigurationAnomalyDetection', got %q", capturedRole)
	}
}

func TestRoleAuthzMiddleware_DeniesUnknownUser(t *testing.T) {
	logger := logrus.New()

	mockAuthz := &ocm.ConfigurableMockAuthorization{
		Permissions: map[string][]string{},
	}

	roles := []RoleMapping{
		{ID: "SREP", AMSResource: "BackplaneOsdSrepResource"},
	}

	middleware := NewRoleAuthzMiddleware(roles, mockAuthz, logger)

	handler := middleware.AuthorizeAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := SetCallerIdentityContext(req.Context(), &CallerIdentity{Username: "unknown-user"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRoleAuthzMiddleware_NoIdentity_Returns401(t *testing.T) {
	logger := logrus.New()

	mockAuthz := &ocm.ConfigurableMockAuthorization{
		Permissions: map[string][]string{},
	}

	roles := []RoleMapping{
		{ID: "SREP", AMSResource: "BackplaneOsdSrepResource"},
	}

	middleware := NewRoleAuthzMiddleware(roles, mockAuthz, logger)

	handler := middleware.AuthorizeAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}
