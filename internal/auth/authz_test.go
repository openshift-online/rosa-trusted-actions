package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/ocm"
)

type mockCatalog struct {
	actions map[string]*Action
}

func (m *mockCatalog) GetAction(name string) (*Action, bool) {
	a, ok := m.actions[name]
	return a, ok
}

func newTestCatalog() *mockCatalog {
	return &mockCatalog{
		actions: map[string]*Action{
			"cluster-info": {
				Name:         "cluster-info",
				AllowedRoles: []string{"SREP", "ConfigurationAnomalyDetection", "ROSAAiAgent"},
			},
			"pod-restart": {
				Name:         "pod-restart",
				AllowedRoles: []string{"SREP"},
			},
		},
	}
}

func setupActionAuthzTest(t *testing.T, catalog ActionCatalog) *chi.Mux {
	t.Helper()
	logger := logrus.New()
	actionAuthz := NewActionAuthzMiddleware(catalog, logger)

	r := chi.NewRouter()
	r.Get("/", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("catalog"))
	})).ServeHTTP)
	r.Post("/{action}/run", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})).ServeHTTP)
	r.Get("/{action}", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP)
	return r
}

func TestActionAuthz_SREPCanRunPodRestart(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
	ctx := SetRoleInContext(req.Context(), "SREP")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_CADCannotRunPodRestart(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
	ctx := SetRoleInContext(req.Context(), "ConfigurationAnomalyDetection")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_CADCanRunClusterInfo(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodPost, "/cluster-info/run", nil)
	ctx := SetRoleInContext(req.Context(), "ConfigurationAnomalyDetection")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_UnknownAction_Returns404(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodPost, "/nonexistent/run", nil)
	ctx := SetRoleInContext(req.Context(), "SREP")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_GETActionChecksRole(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodGet, "/pod-restart", nil)
	ctx := SetRoleInContext(req.Context(), "ConfigurationAnomalyDetection")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for GET on action without required role, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_GETActionAllowedWithRole(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodGet, "/cluster-info", nil)
	ctx := SetRoleInContext(req.Context(), "ConfigurationAnomalyDetection")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET on action with required role, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestActionAuthz_NonActionEndpointPassesThrough(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-action endpoint, got %d; body: %s", rr.Code, rr.Body.String())
	}
}
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
