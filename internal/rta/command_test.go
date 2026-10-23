package rta

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
)

func execute(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	cmd := NewCommand()
	outBuf := &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), err
}

// --- Root command ---

func TestRoot_NoArgs_ShowsHelp(t *testing.T) {
	stdout, err := execute(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Trusted Actions API") {
		t.Errorf("expected help text, got: %s", stdout)
	}
}

func TestRoot_UnknownSubcommand(t *testing.T) {
	_, err := execute(t, "bogus")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"bogus" is not a valid subcommand`) {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestRoot_UnknownSubcommand_Suggestion(t *testing.T) {
	_, err := execute(t, "actio")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `did you mean "action"`) {
		t.Errorf("expected suggestion, got: %s", err)
	}
}

func TestRoot_UnknownFlag(t *testing.T) {
	_, err := execute(t, "--bogus-flag")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("unexpected error: %s", err)
	}
}

// --- Version command ---

func TestVersion_Text(t *testing.T) {
	stdout, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "rta version:") {
		t.Errorf("expected version output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "commit:") {
		t.Errorf("expected commit in output, got: %s", stdout)
	}
}

func TestVersion_JSON(t *testing.T) {
	stdout, err := execute(t, "version", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var info versionInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if info.Version == "" {
		t.Error("version field empty")
	}
	if info.GoVersion == "" {
		t.Error("go_version field empty")
	}
	if info.Platform == "" {
		t.Error("platform field empty")
	}
}

func TestVersion_InvalidFormat(t *testing.T) {
	_, err := execute(t, "version", "-o", "yaml")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid --output") {
		t.Errorf("unexpected error: %s", err)
	}
}

// --- Action list ---

func TestActionList_Text(t *testing.T) {
	catalog := openapi.TrustedActionCatalog{
		Total: 2,
		Items: []openapi.TrustedActionSummary{
			{Name: "get", Scope: "kube-api", Type: "read", Description: "Get resources"},
			{Name: "patch", Scope: "kube-api", Type: "write", Description: "Patch resources"},
		},
	}
	srv := serveCatalog(t, catalog)
	defer srv.Close()

	stdout, err := execute(t, "action", "list", "--server", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "get") || !strings.Contains(stdout, "patch") {
		t.Errorf("expected action names in output: %s", stdout)
	}
	if !strings.Contains(stdout, "NAME") {
		t.Errorf("expected header row: %s", stdout)
	}
}

func TestActionList_JSON(t *testing.T) {
	catalog := openapi.TrustedActionCatalog{
		Total: 1,
		Items: []openapi.TrustedActionSummary{
			{Name: "get", Scope: "kube-api", Type: "read", Description: "Get resources"},
		},
	}
	srv := serveCatalog(t, catalog)
	defer srv.Close()

	stdout, err := execute(t, "action", "list", "--server", srv.URL, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var items []openapi.TrustedActionSummary
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(items) != 1 || items[0].Name != "get" {
		t.Errorf("unexpected items: %+v", items)
	}
}

func TestActionList_Empty(t *testing.T) {
	srv := serveCatalog(t, openapi.TrustedActionCatalog{Items: []openapi.TrustedActionSummary{}})
	defer srv.Close()

	stdout, err := execute(t, "action", "list", "--server", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "No actions found") {
		t.Errorf("expected empty message, got: %s", stdout)
	}
}

func TestActionList_EmptyJSON(t *testing.T) {
	srv := serveCatalog(t, openapi.TrustedActionCatalog{Items: []openapi.TrustedActionSummary{}})
	defer srv.Close()

	stdout, err := execute(t, "action", "list", "--server", srv.URL, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("expected [], got: %s", stdout)
	}
}

// --- Action describe ---

func TestActionDescribe_Text(t *testing.T) {
	required := true
	defVal := "v1"
	desc := "API version"
	action := openapi.TrustedAction{
		Name: "get", Scope: "kube-api", Type: "read", Description: "Get resources",
		Params: &[]openapi.TrustedActionParam{
			{Name: "version", Required: &required, Default: &defVal, Description: &desc},
		},
	}
	srv := serveDescribe(t, action)
	defer srv.Close()

	stdout, err := execute(t, "action", "describe", "get", "--server", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Name:        get") {
		t.Errorf("missing name: %s", stdout)
	}
	if !strings.Contains(stdout, "Parameters:") {
		t.Errorf("missing parameters section: %s", stdout)
	}
	if !strings.Contains(stdout, "version") {
		t.Errorf("missing param name: %s", stdout)
	}
}

func TestActionDescribe_JSON(t *testing.T) {
	action := openapi.TrustedAction{Name: "get", Scope: "kube-api", Type: "read", Description: "Get"}
	srv := serveDescribe(t, action)
	defer srv.Close()

	stdout, err := execute(t, "action", "describe", "get", "--server", srv.URL, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got openapi.TrustedAction
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "get" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestActionDescribe_MissingArg(t *testing.T) {
	_, err := execute(t, "action", "describe")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing required argument: action name") {
		t.Errorf("unexpected error: %s", err)
	}
}

// --- Action run ---

func TestActionRun_MissingFlags(t *testing.T) {
	_, err := execute(t, "action", "run", "get")
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	if !strings.Contains(err.Error(), "cluster") || !strings.Contains(err.Error(), "jira") {
		t.Errorf("expected cluster/jira error, got: %s", err)
	}
}

func TestActionRun_NoWait_Text(t *testing.T) {
	exec := makeExecution("pending")
	srv := serveRunNoWait(t, exec)
	defer srv.Close()

	stdout, err := execute(t, "action", "run", "get",
		"--server", srv.URL, "--token", "tok",
		"--cluster", "c1", "--jira", "OHSS-1", "--no-wait")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "submitted") {
		t.Errorf("expected 'submitted' in output: %s", stdout)
	}
	if !strings.Contains(stdout, exec.Id.String()) {
		t.Errorf("expected execution ID in output: %s", stdout)
	}
}

func TestActionRun_NoWait_JSON(t *testing.T) {
	exec := makeExecution("pending")
	srv := serveRunNoWait(t, exec)
	defer srv.Close()

	stdout, err := execute(t, "action", "run", "get",
		"--server", srv.URL, "--token", "tok",
		"--cluster", "c1", "--jira", "OHSS-1", "--no-wait", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got openapi.Execution
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Id != exec.Id {
		t.Errorf("id = %s", got.Id)
	}
}

func TestActionRun_BadParam(t *testing.T) {
	_, err := execute(t, "action", "run", "get",
		"--server", "http://unused", "--token", "tok",
		"--cluster", "c1", "--jira", "OHSS-1",
		"--param", "noequalssign")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "expected key=value") {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestActionRun_MissingServer(t *testing.T) {
	_, err := execute(t, "action", "run", "get",
		"--server", "", "--token", "tok",
		"--cluster", "c1", "--jira", "OHSS-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server URL is required") {
		t.Errorf("unexpected error: %s", err)
	}
}

// --- Execution list ---

func TestExecutionList_Text(t *testing.T) {
	list := openapi.ExecutionList{
		Items: []openapi.Execution{makeExecution("succeeded")},
		Total: 1,
	}
	srv := serveJSON(t, basePath+"/runs", list)
	defer srv.Close()

	stdout, err := execute(t, "execution", "list", "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ID") {
		t.Errorf("expected header: %s", stdout)
	}
	if !strings.Contains(stdout, "succeeded") {
		t.Errorf("expected status in output: %s", stdout)
	}
}

func TestExecutionList_Empty(t *testing.T) {
	list := openapi.ExecutionList{Items: []openapi.Execution{}}
	srv := serveJSON(t, basePath+"/runs", list)
	defer srv.Close()

	stdout, err := execute(t, "execution", "list", "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "No executions found") {
		t.Errorf("expected empty message: %s", stdout)
	}
}

// --- Execution get ---

func TestExecutionGet_Text(t *testing.T) {
	exec := makeExecution("succeeded")
	srv := serveJSON(t, basePath+"/runs/"+exec.Id.String(), exec)
	defer srv.Close()

	stdout, err := execute(t, "execution", "get", exec.Id.String(), "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ID:") {
		t.Errorf("expected ID label: %s", stdout)
	}
	if !strings.Contains(stdout, "succeeded") {
		t.Errorf("expected status: %s", stdout)
	}
}

func TestExecutionGet_MissingArg(t *testing.T) {
	_, err := execute(t, "execution", "get")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing required argument: execution ID") {
		t.Errorf("unexpected error: %s", err)
	}
}

// --- Execution output ---

func TestExecutionOutput_Text(t *testing.T) {
	output := openapi.ExecutionOutput{
		Message:   "listed 3 pods",
		Resources: []map[string]interface{}{{"kind": "Pod", "name": "p1"}},
	}
	srv := serveJSON(t, basePath+"/runs/abc/output", output)
	defer srv.Close()

	stdout, err := execute(t, "execution", "output", "abc", "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "listed 3 pods") {
		t.Errorf("expected message: %s", stdout)
	}
	if !strings.Contains(stdout, "Resources (1)") {
		t.Errorf("expected resources count: %s", stdout)
	}
}

// --- Audit list ---

func TestAuditList_Text(t *testing.T) {
	action := "get"
	list := openapi.AuditList{
		Items: []openapi.AuditEntry{
			{
				Id:         uuid.New(),
				Timestamp:  time.Now(),
				Username:   "testuser",
				Method:     "POST",
				Path:       "/api/v0/trusted-actions/get/run",
				StatusCode: 202,
				Action:     &action,
			},
		},
		Total: 1,
	}
	srv := serveJSON(t, basePath+"/audit", list)
	defer srv.Close()

	stdout, err := execute(t, "audit", "list", "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "testuser") {
		t.Errorf("expected username: %s", stdout)
	}
	if !strings.Contains(stdout, "TIMESTAMP") {
		t.Errorf("expected header: %s", stdout)
	}
}

func TestAuditList_Empty(t *testing.T) {
	list := openapi.AuditList{Items: []openapi.AuditEntry{}}
	srv := serveJSON(t, basePath+"/audit", list)
	defer srv.Close()

	stdout, err := execute(t, "audit", "list", "--server", srv.URL, "--token", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "No audit entries found") {
		t.Errorf("expected empty message: %s", stdout)
	}
}

// --- Parent commands show help ---

func TestAction_NoSubcommand_ShowsHelp(t *testing.T) {
	stdout, err := execute(t, "action")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Available Commands") {
		t.Errorf("expected help with subcommands: %s", stdout)
	}
}

func TestExecution_NoSubcommand_ShowsHelp(t *testing.T) {
	stdout, err := execute(t, "execution")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Available Commands") {
		t.Errorf("expected help with subcommands: %s", stdout)
	}
}

func TestAudit_NoSubcommand_ShowsHelp(t *testing.T) {
	stdout, err := execute(t, "audit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Available Commands") {
		t.Errorf("expected help with subcommands: %s", stdout)
	}
}

// --- Helpers ---

func makeExecution(status openapi.ExecutionStatus) openapi.Execution {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	dur := 5
	jira := "OHSS-1"
	user := "testuser"
	return openapi.Execution{
		Id:              uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
		Action:          "get",
		TargetCluster:   "c1",
		Status:          status,
		CreatedAt:       now,
		UpdatedAt:       now,
		DurationSeconds: &dur,
		Jira:            &jira,
		Username:        &user,
	}
}

func serveCatalog(t *testing.T, catalog openapi.TrustedActionCatalog) *httptest.Server {
	t.Helper()
	return serveJSON(t, basePath+"/", catalog)
}

func serveDescribe(t *testing.T, action openapi.TrustedAction) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, basePath+"/") {
			_ = json.NewEncoder(w).Encode(action)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func serveRunNoWait(t *testing.T, exec openapi.Execution) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/run") {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(exec)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func serveJSON(t *testing.T, path string, v any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
