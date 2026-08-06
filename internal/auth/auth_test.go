package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/ocm"
)

func setupRouter(t *testing.T, username string) *chi.Mux {
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

	authz := NewRoleAuthzMiddleware(roles, mockAuthz, logger)
	actionAuthz := NewActionAuthzMiddleware(catalog, logger)

	r := chi.NewRouter()
	r.Use(testAuthn(username))
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
	r := setupRouter(t, "srep-user")

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
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
	r := setupRouter(t, "cad-user")

	req := httptest.NewRequest(http.MethodPost, "/pod-restart/run", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCanExecuteClusterInfo(t *testing.T) {
	r := setupRouter(t, "cad-user")

	req := httptest.NewRequest(http.MethodPost, "/cluster-info/run", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_NoAuthHeader_Returns401(t *testing.T) {
	r := setupRouter(t, "")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_UnauthorizedUser_Returns403(t *testing.T) {
	r := setupRouter(t, "random-user")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_SREPCanAccessCatalog(t *testing.T) {
	r := setupRouter(t, "srep-user")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCanDescribeClusterInfo(t *testing.T) {
	r := setupRouter(t, "cad-user")

	req := httptest.NewRequest(http.MethodGet, "/cluster-info", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_CADCannotDescribePodRestart(t *testing.T) {
	r := setupRouter(t, "cad-user")

	req := httptest.NewRequest(http.MethodGet, "/pod-restart", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func testAuthn(username string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if username == "" {
				next.ServeHTTP(w, r)
				return
			}
			ctx := SetCallerIdentityContext(r.Context(), &CallerIdentity{
				Username: username,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
