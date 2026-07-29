package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
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
				Name:          "cluster-info",
				RequiredRoles: []string{"SREP", "ConfigurationAnomalyDetection", "ROSAAiAgent"},
			},
			"pod-restart": {
				Name:          "pod-restart",
				RequiredRoles: []string{"SREP"},
			},
		},
	}
}

func setupActionAuthzTest(t *testing.T, catalog ActionCatalog) *chi.Mux {
	t.Helper()
	logger := logrus.New()
	actionAuthz := NewActionAuthzMiddleware(catalog, logger)

	r := chi.NewRouter()
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

func TestActionAuthz_GETPassesThrough(t *testing.T) {
	r := setupActionAuthzTest(t, newTestCatalog())

	req := httptest.NewRequest(http.MethodGet, "/cluster-info", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET (no action authz enforcement), got %d", rr.Code)
	}
}
