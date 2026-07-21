package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/ocm"
)

func setupIntegrationRouter(t *testing.T) *chi.Mux {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	mockAuthz := &ocm.ConfigurableMockAuthorization{
		Permissions: map[string][]string{
			"srep-user": {"BackplaneOsdSrepResource"},
			"cad-user":  {"BackplaneOsdCadResource"},
		},
	}

	roles := []RoleMapping{
		{ID: "SREP", AMSResource: "BackplaneOsdSrepResource"},
		{ID: "ConfigurationAnomalyDetection", AMSResource: "BackplaneOsdCadResource"},
		{ID: "ROSAAiAgent", AMSResource: "BackplaneOsdAiAgentResource"},
	}

	catalog := newTestCatalog()

	authn := NewMockAuthMiddleware(logger)
	authz := NewRoleAuthzMiddleware(roles, mockAuthz, logger)
	actionAuthz := NewActionAuthzMiddleware(catalog, logger)

	r := chi.NewRouter()
	r.Use(authn.AuthenticateAccountJWT)
	r.Use(authz.AuthorizeAPI)

	r.Get("/", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("catalog"))
	})).ServeHTTP)

	r.Get("/{action}", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("describe"))
	})).ServeHTTP)

	r.Post("/{action}/run", actionAuthz.CheckActionAccess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := GetCallerIdentityFromContext(r.Context())
		role := GetRoleFromContext(r.Context())
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("executed by " + identity.Username + " as " + role))
	})).ServeHTTP)

	return r
}

func TestIntegration_SREPCanExecutePodRestart(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
	req.Header.Set("X-Mock-Username", "srep-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); body != "executed by srep-user as SREP" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestIntegration_CADCannotExecutePodRestart(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
	req.Header.Set("X-Mock-Username", "cad-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCanExecuteClusterInfo(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/cluster-info/run", nil)
	req.Header.Set("X-Mock-Username", "cad-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_NoAuthHeader_Returns401(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_UnauthorizedUser_Returns403(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Mock-Username", "random-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_SREPCanAccessCatalog(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Mock-Username", "srep-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCanDescribeClusterInfo(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/cluster-info", nil)
	req.Header.Set("X-Mock-Username", "cad-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCannotDescribePodRestart(t *testing.T) {
	r := setupIntegrationRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/pod-restart", nil)
	req.Header.Set("X-Mock-Username", "cad-user")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}
