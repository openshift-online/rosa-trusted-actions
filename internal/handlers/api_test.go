package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	acctrspv1 "github.com/openshift-online/ocm-sdk-go/accesstransparency/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openshift-online/rosa-trusted-actions/internal/actions"
	"github.com/openshift-online/rosa-trusted-actions/internal/catalog"
	"github.com/openshift-online/rosa-trusted-actions/internal/models"
	"github.com/openshift-online/rosa-trusted-actions/internal/ocm"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
	"github.com/openshift-online/rosa-trusted-actions/internal/worker"
)

// fakeNotifier records Notify calls for assertions.
type fakeNotifier struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeNotifier) Notify() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
}

func (f *fakeNotifier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type runnerCallback func(ctx context.Context, exec *models.Execution) worker.RunResult

type fakeRunner struct {
	callback runnerCallback
}

func (f *fakeRunner) Run(ctx context.Context, exec *models.Execution) worker.RunResult {
	return f.callback(ctx, exec)
}

func newTestHandler(t *testing.T, isAccessProtectionEnabled bool, accessRequest *acctrspv1.AccessRequest, runnerCallback runnerCallback) *APIHandler {
	t.Helper()
	s, err := store.NewSQLiteStore(context.Background(), ":memory:", logrus.New())
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("failed to close test store: %v", err)
		}
	})

	var runner worker.Runner

	if runnerCallback != nil {
		runner = &fakeRunner{callback: runnerCallback}
	}

	return NewAPIHandler(logrus.New(), catalog.New(), &ocm.ConfigurableMockAccessProtection{
		IsEnabled:     isAccessProtectionEnabled,
		AccessRequest: accessRequest,
	}, runner, s, &fakeNotifier{})
}

func TestAPIHandler_Catalog(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/", nil)
	w := httptest.NewRecorder()

	handler.Catalog(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var catalog openapi.TrustedActionCatalog
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if catalog.Total != 6 {
		t.Errorf("Expected 6 actions, got %d", catalog.Total)
	}

	if len(catalog.Items) != 6 {
		t.Errorf("Expected 6 items, got %d", len(catalog.Items))
	}
}

func TestAPIHandler_Describe(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/get", nil)
	w := httptest.NewRecorder()

	handler.Describe(w, req, "get")

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var action openapi.TrustedAction
	if err := json.Unmarshal(w.Body.Bytes(), &action); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if action.Name != "get" {
		t.Errorf("Expected action name 'get', got %s", action.Name)
	}

	if action.Type != openapi.Read {
		t.Errorf("Expected action type 'read', got %s", action.Type)
	}
}

func TestAPIHandler_CreateExecution(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	requestBody := `{
		"target_cluster": "test-cluster",
		"jira": "ROSAENG-1234",
		"params": {"namespace": "default"},
		"dry_run": true
	}`

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	replyMode := openapi.Async
	handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	var reply openapi.ExecutionWithOutput
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if reply.Execution.Action != "get" {
		t.Errorf("Expected action 'get', got %s", reply.Execution.Action)
	}

	if reply.Execution.Status != openapi.ExecutionStatusPending {
		t.Errorf("Expected status 'pending', got %s", reply.Execution.Status)
	}

	if reply.Execution.TargetCluster != "test-cluster" {
		t.Errorf("Expected target cluster 'test-cluster', got %s", reply.Execution.TargetCluster)
	}
}

func TestAPIHandler_CreateAndRunExecution(t *testing.T) {
	createAccessRequest := func(state acctrspv1.AccessRequestState) *acctrspv1.AccessRequest {
		g := NewWithT(t)
		accessRequest, err := acctrspv1.NewAccessRequest().Status(
			acctrspv1.NewAccessRequestStatus().State(state),
		).Build()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(accessRequest).ToNot(BeNil())

		return accessRequest
	}

	tests := []struct {
		testName                         string
		replyMode                        openapi.CreateExecutionParamsReplyMode
		isAccessProtectionEnabled        bool
		accessRequest                    *acctrspv1.AccessRequest
		isExpectedToRun                  bool
		isRunningInError                 bool
		expectedResponseStatusCode       int
		expectedExecutionStatus          openapi.ExecutionStatus
		isExpectingOutputInResponse      bool
		expectedStoredNotificationsCount int
		isExpectingExecutionInStore      bool
		isExpectingOutputInStore         bool
	}{
		// Access protection disabled
		{
			testName:                    "When access protection is disabled and the reply mode is default, execution is created and run as well",
			replyMode:                   openapi.Default,
			isExpectedToRun:             true,
			expectedResponseStatusCode:  http.StatusAccepted,
			expectedExecutionStatus:     openapi.ExecutionStatusSucceeded,
			isExpectingOutputInResponse: true,
			isExpectingExecutionInStore: true,
			isExpectingOutputInStore:    true,
		},
		{
			testName:                    "When access protection is disabled and the reply mode is synchronous, execution is created and run as well",
			replyMode:                   openapi.Sync,
			isExpectedToRun:             true,
			expectedResponseStatusCode:  http.StatusAccepted,
			expectedExecutionStatus:     openapi.ExecutionStatusSucceeded,
			isExpectingOutputInResponse: true,
			isExpectingExecutionInStore: true,
			isExpectingOutputInStore:    true,
		},
		// No active access request
		{
			testName:                         "When access protection is enabled but there is no active access request and the reply mode is default, execution is created but will run asynchronously",
			replyMode:                        openapi.Default,
			isAccessProtectionEnabled:        true,
			expectedResponseStatusCode:       http.StatusAccepted,
			expectedExecutionStatus:          openapi.ExecutionStatusPending,
			expectedStoredNotificationsCount: 1,
			isExpectingExecutionInStore:      true,
		},
		{
			testName:                   "When access protection is enabled but there is no active access request and the reply mode is synchronous, execution won't be created",
			replyMode:                  openapi.Sync,
			isAccessProtectionEnabled:  true,
			expectedResponseStatusCode: http.StatusBadRequest,
		},
		// Pending access request
		{
			testName:                         "When the access request is pending and the reply mode is default, execution is created but will run asynchronously",
			replyMode:                        openapi.Default,
			isAccessProtectionEnabled:        true,
			accessRequest:                    createAccessRequest(acctrspv1.AccessRequestStatePending),
			expectedResponseStatusCode:       http.StatusAccepted,
			expectedExecutionStatus:          openapi.ExecutionStatusPending,
			expectedStoredNotificationsCount: 1,
			isExpectingExecutionInStore:      true,
		},
		{
			testName:                   "When the access request is pending and the reply mode is synchronous, execution won't be created",
			replyMode:                  openapi.Sync,
			isAccessProtectionEnabled:  true,
			accessRequest:              createAccessRequest(acctrspv1.AccessRequestStatePending),
			expectedResponseStatusCode: http.StatusBadRequest,
		},
		// Approved access request
		{
			testName:                    "When the access request is approved and the reply mode is default, execution is created and run as well",
			replyMode:                   openapi.Default,
			isAccessProtectionEnabled:   true,
			accessRequest:               createAccessRequest(acctrspv1.AccessRequestStateApproved),
			isExpectedToRun:             true,
			expectedResponseStatusCode:  http.StatusAccepted,
			expectedExecutionStatus:     openapi.ExecutionStatusSucceeded,
			isExpectingOutputInResponse: true,
			isExpectingExecutionInStore: true,
			isExpectingOutputInStore:    true,
		},
		{
			testName:                    "When the access request is approved and the reply mode is synchronous, execution is created and run as well",
			replyMode:                   openapi.Sync,
			isAccessProtectionEnabled:   true,
			accessRequest:               createAccessRequest(acctrspv1.AccessRequestStateApproved),
			isExpectedToRun:             true,
			expectedResponseStatusCode:  http.StatusAccepted,
			expectedExecutionStatus:     openapi.ExecutionStatusSucceeded,
			isExpectingOutputInResponse: true,
			isExpectingExecutionInStore: true,
			isExpectingOutputInStore:    true,
		},
		// Execution run ends up in error
		{
			testName:                   "When running the execution returns an error and the reply mode is default, execution will still be created",
			replyMode:                  openapi.Default,
			isExpectedToRun:            true,
			isRunningInError:           true,
			expectedResponseStatusCode: http.StatusInternalServerError,
			expectedExecutionStatus:    openapi.ExecutionStatusFailed,
		},
		{
			testName:                   "When running the execution returns an error and the reply mode is synchronous, execution will still be created",
			replyMode:                  openapi.Sync,
			isExpectedToRun:            true,
			isRunningInError:           true,
			expectedResponseStatusCode: http.StatusInternalServerError,
			expectedExecutionStatus:    openapi.ExecutionStatusFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			g := NewWithT(t)

			cm := map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"namespace": "default",
					"name":      "test-configmap",
				},
			}

			hasRun := false
			handler := newTestHandler(t, test.isAccessProtectionEnabled, test.accessRequest,
				func(ctx context.Context, exec *models.Execution) worker.RunResult {
					hasRun = true
					if test.isRunningInError {
						return worker.RunResult{
							Status: "failed",
							Reason: "Execution failed due to some error",
						}
					} else {
						return worker.RunResult{
							Status: "succeeded",
							Output: &actions.ActionResult{
								Message: "Execution completed successfully",
								Resources: []unstructured.Unstructured{{
									Object: cm,
								}},
							},
						}
					}
				})

			requestBody := `{
				"target_cluster": "test-cluster",
				"jira": "ROSAENG-1234",
				"params": {"namespace": "default"},
				"dry_run": true
			}`

			req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			replyMode := test.replyMode
			handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})
			var execId uuid.UUID

			g.Expect(hasRun).To(Equal(test.isExpectedToRun), "expected execution to be run: %v, but got %v", test.isExpectedToRun, hasRun)
			g.Expect(w.Code).To(Equal(test.expectedResponseStatusCode))

			if test.expectedResponseStatusCode == http.StatusAccepted {
				var reply openapi.ExecutionWithOutput
				err := json.Unmarshal(w.Body.Bytes(), &reply)
				g.Expect(err).ToNot(HaveOccurred())

				execId = reply.Execution.Id
				g.Expect(reply.Execution.Action).To(Equal("get"))
				g.Expect(reply.Execution.TargetCluster).To(Equal("test-cluster"))
				g.Expect(reply.Execution.Status).To(Equal(test.expectedExecutionStatus))

				if test.isExpectingOutputInResponse {
					g.Expect(reply.Output).ToNot(BeNil())
					g.Expect(reply.Output.Message).To(Equal("Execution completed successfully"))
					g.Expect(reply.Output.Resources).To(Equal([]map[string]interface{}{cm}))
				} else {
					g.Expect(reply.Output).To(BeNil())
				}
			}

			notifier, ok := handler.notifier.(*fakeNotifier)
			g.Expect(ok).To(BeTrue(), "expected notifier to be *fakeNotifier")

			g.Expect(notifier.callCount()).To(Equal(test.expectedStoredNotificationsCount))

			dbExecList, err := handler.store.ListExecutions(t.Context(), store.ExecutionFilter{})
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(dbExecList).ToNot(BeNil())

			if test.isExpectingExecutionInStore {
				g.Expect(len(dbExecList.Items)).To(Equal(1))
				dbExec := dbExecList.Items[0]
				g.Expect(dbExec.ID).To(Equal(execId))
				g.Expect(dbExec.Action).To(Equal("get"))
				g.Expect(dbExec.TargetCluster).To(Equal("test-cluster"))
				g.Expect(dbExec.Status).To(Equal(string(test.expectedExecutionStatus)))
			} else {
				g.Expect(dbExecList.Items).To(BeEmpty())
			}

			dbOutput, err := handler.store.GetExecutionOutput(t.Context(), execId)

			if test.isExpectingOutputInStore {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dbOutput).ToNot(BeNil())

			} else {
				g.Expect(dbOutput).To(BeNil())
			}
		})
	}
}

func TestAPIHandler_CreateExecution_NotifiesWorker(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)
	notifier, ok := handler.notifier.(*fakeNotifier)
	if !ok {
		t.Fatalf("expected notifier to be *fakeNotifier, got %T", handler.notifier)
	}

	requestBody := `{"target_cluster": "test-cluster", "jira": "ROSAENG-1234"}`
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	replyMode := openapi.Async
	handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected status 202, got %d", w.Code)
	}
	if got := notifier.callCount(); got != 1 {
		t.Errorf("Notify calls: got %d, want 1", got)
	}
}

func TestAPIHandler_CreateExecution_DoesNotNotifyOnStoreError(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)
	if err := handler.store.Close(); err != nil { // Close the store to simulate a store error
		t.Fatalf("failed to close test store: %v", err)
	}

	requestBody := `{"target_cluster": "test-cluster", "jira": "ROSAENG-1234"}`
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	replyMode := openapi.Async
	handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}
	if got := handler.notifier.(*fakeNotifier).callCount(); got != 0 {
		t.Errorf("Notify calls: got %d, want 0", got)
	}
}

func TestAPIHandler_CreateExecution_InvalidJSON(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	requestBody := `{"invalid": json}`

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	replyMode := openapi.Async
	handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var errorResp openapi.Error
	if err := json.Unmarshal(w.Body.Bytes(), &errorResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if errorResp.Kind != openapi.ErrorKindError {
		t.Errorf("Expected error kind 'Error', got %s", errorResp.Kind)
	}
}

func TestAPIHandler_GetExecution_NotFound(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	id := uuid.New()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs/"+id.String(), nil)
	w := httptest.NewRecorder()

	handler.GetExecution(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestAPIHandler_GetExecution_Found(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	requestBody := `{
		"target_cluster": "test-cluster",
		"jira": "ROSAENG-1234"
	}`
	createReq := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	replyMode := openapi.Async
	handler.CreateExecution(createW, createReq, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})

	if createW.Code != http.StatusAccepted {
		t.Fatalf("CreateExecution: expected status 202, got %d", createW.Code)
	}

	var created openapi.ExecutionWithOutput
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	getReq := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs/"+created.Execution.Id.String(), nil)
	getW := httptest.NewRecorder()
	handler.GetExecution(getW, getReq, created.Execution.Id)

	if getW.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", getW.Code)
	}

	var got openapi.Execution
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if got.Id != created.Execution.Id {
		t.Errorf("Expected ID %s, got %s", created.Execution.Id, got.Id)
	}
	if got.Action != "get" {
		t.Errorf("Expected action 'get', got %s", got.Action)
	}
}

func TestAPIHandler_GetExecutionOutput_NotFound(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	id := uuid.New()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs/"+id.String()+"/output", nil)
	w := httptest.NewRecorder()

	handler.GetExecutionOutput(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestAPIHandler_GetExecutionOutput_Found(t *testing.T) {
	g := NewWithT(t)
	handler := newTestHandler(t, false, nil, nil)

	requestBody := `{
		"target_cluster": "test-cluster",
		"jira": "ROSAENG-1234"
	}`
	createReq := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(requestBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	replyMode := openapi.Async
	handler.CreateExecution(createW, createReq, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})
	g.Expect(createW.Code).To(Equal(http.StatusAccepted))

	var created openapi.ExecutionWithOutput
	err := json.Unmarshal(createW.Body.Bytes(), &created)
	g.Expect(err).ToNot(HaveOccurred())

	message := "execution logs"
	resources := []map[string]interface{}{
		{
			"name":     "object1",
			"data":     "some data",
			"some-key": "some value",
		},
		{
			"name":   "object2",
			"data":   "some other data",
			"secret": "can't say",
		},
	}
	err = handler.store.UpdateExecutionWithResult(context.Background(), created.Execution.Id, "succeeded", nil, &models.ExecutionOutput{
		Message:   message,
		Resources: resources,
	})
	g.Expect(err).ToNot(HaveOccurred())

	getOutputReq := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs/"+created.Execution.Id.String()+"/output", nil)
	getOutputW := httptest.NewRecorder()
	handler.GetExecutionOutput(getOutputW, getOutputReq, created.Execution.Id)
	g.Expect(getOutputW.Code).To(Equal(http.StatusOK))

	var got openapi.ExecutionOutput
	err = json.Unmarshal(getOutputW.Body.Bytes(), &got)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got.Message).To(Equal(message))
	g.Expect(got.Resources).To(Equal(resources))
}

func TestAPIHandler_ListExecutions_Empty(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs", nil)
	w := httptest.NewRecorder()

	handler.ListExecutions(w, req, openapi.ListExecutionsParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.ExecutionList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if list.Total != 0 {
		t.Errorf("Expected total 0, got %d", list.Total)
	}
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(list.Items))
	}
}

func TestAPIHandler_ListExecutions_WithResults(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	for _, cluster := range []string{"cluster-1", "cluster-2"} {
		body := `{"target_cluster": "` + cluster + `", "jira": "ROSAENG-1234"}`
		req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		replyMode := openapi.Async
		handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})
		if w.Code != http.StatusAccepted {
			t.Fatalf("CreateExecution for %s: expected 202, got %d", cluster, w.Code)
		}
	}

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs", nil)
	w := httptest.NewRecorder()
	handler.ListExecutions(w, req, openapi.ListExecutionsParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.ExecutionList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if list.Total != 2 {
		t.Errorf("Expected total 2, got %d", list.Total)
	}
	if len(list.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(list.Items))
	}
}

func TestAPIHandler_ListExecutions_Pagination(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	for i := 0; i < 3; i++ {
		body := `{"target_cluster": "cluster", "jira": "ROSAENG-1234"}`
		req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		replyMode := openapi.Async
		handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})
		if w.Code != http.StatusAccepted {
			t.Fatalf("CreateExecution[%d]: expected 202, got %d", i, w.Code)
		}
	}

	limit := 2
	page1 := 1
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?limit=2&page=1", nil)
	w := httptest.NewRecorder()
	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Limit: &limit, Page: &page1})

	if w.Code != http.StatusOK {
		t.Fatalf("Page 1: expected 200, got %d", w.Code)
	}

	var list1 openapi.ExecutionList
	if err := json.Unmarshal(w.Body.Bytes(), &list1); err != nil {
		t.Fatalf("Failed to parse page 1: %v", err)
	}
	if list1.Total != 3 {
		t.Errorf("Page 1 Total: got %d, want 3", list1.Total)
	}
	if len(list1.Items) != 2 {
		t.Errorf("Page 1 Items: got %d, want 2", len(list1.Items))
	}
	if list1.Page != 1 {
		t.Errorf("Page 1 Page: got %d, want 1", list1.Page)
	}
	if !list1.HasMore {
		t.Error("Page 1 HasMore: got false, want true")
	}

	page2 := 2
	req2 := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?limit=2&page=2", nil)
	w2 := httptest.NewRecorder()
	handler.ListExecutions(w2, req2, openapi.ListExecutionsParams{Limit: &limit, Page: &page2})

	if w2.Code != http.StatusOK {
		t.Fatalf("Page 2: expected 200, got %d", w2.Code)
	}

	var list2 openapi.ExecutionList
	if err := json.Unmarshal(w2.Body.Bytes(), &list2); err != nil {
		t.Fatalf("Failed to parse page 2: %v", err)
	}
	if list2.Total != 3 {
		t.Errorf("Page 2 Total: got %d, want 3", list2.Total)
	}
	if len(list2.Items) != 1 {
		t.Errorf("Page 2 Items: got %d, want 1", len(list2.Items))
	}
	if list2.Page != 2 {
		t.Errorf("Page 2 Page: got %d, want 2", list2.Page)
	}
	if list2.HasMore {
		t.Error("Page 2 HasMore: got true, want false")
	}
}

func TestAPIHandler_ListExecutions_PageWithoutLimit(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	// Create 3 executions
	for i := 0; i < 3; i++ {
		body := `{"target_cluster": "cluster", "jira": "ROSAENG-1234"}`
		req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v0/trusted-actions/get/run", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		replyMode := openapi.Async
		handler.CreateExecution(w, req, "get", openapi.CreateExecutionParams{ReplyMode: &replyMode})
		if w.Code != http.StatusAccepted {
			t.Fatalf("CreateExecution[%d]: expected 202, got %d", i, w.Code)
		}
	}

	// Request page 1 without limit (should use default 20, return all 3 items)
	page1 := 1
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?page=1", nil)
	w := httptest.NewRecorder()
	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Page: &page1})

	if w.Code != http.StatusOK {
		t.Fatalf("Page 1: expected 200, got %d", w.Code)
	}

	var list openapi.ExecutionList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse page 1: %v", err)
	}
	if list.Total != 3 {
		t.Errorf("Page 1 Total: got %d, want 3", list.Total)
	}
	if len(list.Items) != 3 {
		t.Errorf("Page 1 Items: got %d, want 3", len(list.Items))
	}
	if list.Limit != 20 {
		t.Errorf("Page 1 Limit: got %d, want 20 (default)", list.Limit)
	}
	if list.Page != 1 {
		t.Errorf("Page 1 Page: got %d, want 1", list.Page)
	}
}

func TestAPIHandler_ListExecutions_InvalidPage(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	// Test page < 1
	page0 := 0
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?page=0", nil)
	w := httptest.NewRecorder()
	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Page: &page0})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Page 0: expected 400, got %d", w.Code)
	}

	// Test page overflow (large page * limit would overflow int)
	largeLimit := 100
	largePage := 25000000 // 25M * 100 = 2.5B > maxint32
	req2 := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?page=25000000&limit=100", nil)
	w2 := httptest.NewRecorder()
	handler.ListExecutions(w2, req2, openapi.ListExecutionsParams{Page: &largePage, Limit: &largeLimit})

	if w2.Code != http.StatusBadRequest {
		t.Errorf("Large page: expected 400, got %d", w2.Code)
	}
}

func TestAPIHandler_ListAuditEntries_Empty(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/audit", nil)
	w := httptest.NewRecorder()

	handler.ListAuditEntries(w, req, openapi.ListAuditEntriesParams{})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var list openapi.AuditList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if list.Total != 0 {
		t.Errorf("Expected total 0, got %d", list.Total)
	}
}

func TestAPIHandler_ListExecutions_NegativeSince(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	since := "-24h"
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?since=-24h", nil)
	w := httptest.NewRecorder()

	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Since: &since})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestAPIHandler_ListExecutions_ZeroSince(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	since := "0h"
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs?since=0h", nil)
	w := httptest.NewRecorder()

	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Since: &since})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestAPIHandler_ListExecutions_OverflowSince(t *testing.T) {
	handler := newTestHandler(t, false, nil, nil)

	since := "999999999999d"
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v0/trusted-actions/runs", nil)
	w := httptest.NewRecorder()

	handler.ListExecutions(w, req, openapi.ListExecutionsParams{Since: &since})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}
