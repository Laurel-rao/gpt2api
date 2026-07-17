package videoworkflow

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"github.com/432539/gpt2api/internal/middleware"
)

func TestHandler_WorkflowAndRunEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore(t)
	service := NewService(store)
	service.SetRuntime(&fakeRuntime{})
	workflow, err := service.CreateWorkflow(t.Context(), CreateWorkflowInput{UserID: 7, TemplateID: 1, Name: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.GET("/templates", handler.ListTemplates)
	router.GET("/runtime-settings", handler.GetRuntimeSettings)
	router.PUT("/runtime-settings", handler.UpdateRuntimeSettings)
	router.POST("/workflows", handler.CreateWorkflow)
	router.GET("/workflows", handler.ListWorkflows)
	router.GET("/workflows/:id", handler.GetWorkflow)
	router.PUT("/workflows/:id", handler.UpdateWorkflow)
	router.DELETE("/workflows/:id", handler.DeleteWorkflow)
	router.POST("/workflows/:id/validate", handler.ValidateWorkflow)
	router.POST("/workflows/:id/estimate", handler.EstimateRun)
	router.POST("/workflows/:id/runs", handler.StartRun)
	router.GET("/workflows/:id/runs", handler.ListRuns)
	router.GET("/runs/:run_id", handler.GetRun)
	router.POST("/runs/:run_id/cancel", handler.CancelRun)
	router.POST("/runs/:run_id/characters", handler.ApproveCharacters)
	router.POST("/runs/:run_id/storyboard", handler.ApproveStoryboard)
	router.POST("/runs/:run_id/approve", handler.ApproveRun)

	assertHTTP(t, router, http.MethodGet, "/templates", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodGet, "/runtime-settings", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodPut, "/runtime-settings", RuntimeConcurrency{WorkerConcurrency: 5, TextConcurrency: 3, ImageConcurrency: 4, VideoConcurrency: 2, ComposeConcurrency: 1}, http.StatusOK)
	assertHTTP(t, router, http.MethodGet, "/workflows", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodGet, "/workflows/"+workflow.ID, nil, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/validate", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/estimate", nil, http.StatusOK)

	update := map[string]any{"revision": workflow.Revision, "name": "已更新", "graph": workflow.Graph}
	assertHTTP(t, router, http.MethodPut, "/workflows/"+workflow.ID, update, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/runs", nil, http.StatusOK)
	runID := store.run.ID
	assertHTTP(t, router, http.MethodGet, "/workflows/"+workflow.ID+"/runs?limit=20&offset=0", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodGet, "/runs/"+runID, nil, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/runs/"+runID+"/cancel", nil, http.StatusOK)

	store.run.Status = RunAwaitingCharacterApproval
	characters := CharacterApproval{Selections: []CharacterSelection{{NodeRunID: "node", InputHash: "hash", SelectedVersionID: "version"}}}
	assertHTTP(t, router, http.MethodPost, "/runs/"+runID+"/characters", characters, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/runs/"+runID+"/approve", map[string]any{"approval_type": "characters", "selections": characters.Selections}, http.StatusOK)

	store.run.Status = RunAwaitingStoryboardApproval
	storyboard := StoryboardApproval{NodeRunID: "node", InputHash: "hash", Script: json.RawMessage(`{"scenes":[]}`)}
	assertHTTP(t, router, http.MethodPost, "/runs/"+runID+"/storyboard", storyboard, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/runs/"+runID+"/approve", map[string]any{"approval_type": "storyboard", "node_run_id": "node", "input_hash": "hash", "script": map[string]any{"scenes": []any{}}}, http.StatusOK)

	create := map[string]any{"template_id": 1, "name": "新建"}
	assertHTTP(t, router, http.MethodPost, "/workflows", create, http.StatusOK)
	assertHTTP(t, router, http.MethodDelete, "/workflows/"+store.workflow.ID, nil, http.StatusOK)
}

func TestHandler_ErrorMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore(t)
	service := NewService(store)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.GET("/workflows/:id", handler.GetWorkflow)
	router.POST("/workflows", handler.CreateWorkflow)
	router.POST("/runs/:run_id/approve", handler.ApproveRun)
	assertHTTP(t, router, http.MethodGet, "/workflows/missing", nil, http.StatusNotFound)
	assertHTTP(t, router, http.MethodPost, "/workflows", map[string]any{}, http.StatusBadRequest)
	assertHTTP(t, router, http.MethodPost, "/runs/missing/approve", map[string]any{"approval_type": "unknown"}, http.StatusBadRequest)
}

func TestHandler_WorkflowValidationAndStateErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(t.Context(), CreateWorkflowInput{UserID: 7, TemplateID: 1, Name: "测试"})
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.PUT("/workflows/:id", handler.UpdateWorkflow)
	router.DELETE("/workflows/:id", handler.DeleteWorkflow)
	router.POST("/workflows/:id/validate", handler.ValidateWorkflow)
	router.POST("/workflows/:id/estimate", handler.EstimateRun)
	router.POST("/workflows/:id/runs", handler.StartRun)
	router.GET("/runs/:run_id", handler.GetRun)
	router.POST("/runs/:run_id/cancel", handler.CancelRun)
	router.POST("/runs/:run_id/characters", handler.ApproveCharacters)
	router.POST("/runs/:run_id/storyboard", handler.ApproveStoryboard)

	conflict := map[string]any{"revision": 99, "name": "冲突", "graph": workflow.Graph}
	assertHTTP(t, router, http.MethodPut, "/workflows/"+workflow.ID, conflict, http.StatusConflict)
	invalid, _ := CloneGraph(workflow.Graph)
	invalid.Edges[0].TargetPort = "missing"
	badGraph := map[string]any{"revision": 1, "name": "错误", "graph": invalid}
	assertHTTP(t, router, http.MethodPut, "/workflows/"+workflow.ID, badGraph, http.StatusUnprocessableEntity)
	assertHTTP(t, router, http.MethodDelete, "/workflows/missing", nil, http.StatusNotFound)
	assertHTTP(t, router, http.MethodPost, "/workflows/missing/validate", nil, http.StatusNotFound)
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/estimate", nil, http.StatusInternalServerError)

	store.workflow.Graph.Edges = nil
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/runs", nil, http.StatusUnprocessableEntity)
	assertHTTP(t, router, http.MethodGet, "/runs/missing", nil, http.StatusNotFound)
	store.run = &Run{ID: "run", UserID: 7, Status: RunSucceeded}
	assertHTTP(t, router, http.MethodPost, "/runs/run/cancel", nil, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/runs/run/characters", CharacterApproval{Selections: []CharacterSelection{{NodeRunID: "node"}}}, http.StatusOK)
	assertHTTP(t, router, http.MethodPost, "/runs/run/storyboard", StoryboardApproval{NodeRunID: "node", InputHash: "hash", Script: []byte(`{}`)}, http.StatusOK)
}

func TestHandler_StartRunRequiresEstimateTokenForRuntimeV2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore(t)
	service := NewService(store)
	workflow, _ := service.CreateWorkflow(t.Context(), CreateWorkflowInput{UserID: 7, TemplateID: 1})
	service.SetRuntime(&strictFakeRuntime{})
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.POST("/workflows/:id/runs", handler.StartRun)
	body, _ := json.Marshal(StartRunInput{Revision: workflow.Revision, RunMode: RunModeFull, RequestID: "request-without-estimate"})
	request := httptest.NewRequest(http.MethodPost, "/workflows/"+workflow.ID+"/runs", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var result struct {
		Code int `json:"code"`
	}
	if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Code != 40000 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandler_RequestIDConflictReturnsHTTP409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	handleError(context, fmt.Errorf("%w: revision differs", ErrRequestConflict))
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandler_UploadPNGInfersKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registerFakeDB.Do(func() { sql.Register("video-workflow-fake", fakeSQLDriver{}) })
	db, err := sqlx.Open("video-workflow-fake", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(NewDAO(db))
	service.ConfigureMedia(t.TempDir(), "test-secret", NewComposer())
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.POST("/assets", handler.UploadAsset)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "frame.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(makeRuntimePNG(t))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var result struct {
		Code int `json:"code"`
		Data struct {
			Kind     string `json:"kind"`
			Versions []struct {
				PreviewURL string `json:"preview_url"`
			} `json:"versions"`
		} `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Code != 0 || result.Data.Kind != MediaKindImage || len(result.Data.Versions) != 1 || result.Data.Versions[0].PreviewURL == "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandler_ValidateWorkflowAcceptsOptionalGraph(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore(t)
	service := NewService(store)
	workflow, err := service.CreateWorkflow(t.Context(), CreateWorkflowInput{UserID: 7, TemplateID: 1, Name: "校验"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(7)); c.Next() })
	router.POST("/workflows/:id/validate", handler.ValidateWorkflow)

	assertValidateResponse(t, router, "/workflows/"+workflow.ID+"/validate", nil, http.StatusOK, true, "")
	assertValidateResponse(t, router, "/workflows/"+workflow.ID+"/validate", map[string]any{}, http.StatusOK, true, "")

	invalid, err := CloneGraph(workflow.Graph)
	if err != nil {
		t.Fatal(err)
	}
	invalid.Edges[0].TargetPort = "missing"
	assertValidateResponse(t, router, "/workflows/"+workflow.ID+"/validate", map[string]any{"graph": invalid}, http.StatusOK, false, string(ValidationPortNotFound))
	assertValidateResponse(t, router, "/workflows/"+workflow.ID+"/validate", map[string]any{"graph": workflow.Graph}, http.StatusOK, true, "")

	assertHTTP(t, router, http.MethodPost, "/workflows/missing/validate", map[string]any{"graph": workflow.Graph}, http.StatusNotFound)
	assertHTTP(t, router, http.MethodPost, "/workflows/"+workflow.ID+"/validate", map[string]any{"graph": "bad"}, http.StatusBadRequest)

	other := gin.New()
	other.Use(func(c *gin.Context) { c.Set(middleware.CtxUserID, uint64(8)); c.Next() })
	other.POST("/workflows/:id/validate", handler.ValidateWorkflow)
	assertHTTP(t, other, http.MethodPost, "/workflows/"+workflow.ID+"/validate", map[string]any{"graph": workflow.Graph}, http.StatusNotFound)
	assertHTTP(t, other, http.MethodPost, "/workflows/"+workflow.ID+"/validate", nil, http.StatusNotFound)
}

func assertValidateResponse(t *testing.T, router http.Handler, path string, body any, status int, wantValid bool, wantCode string) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Valid  bool              `json:"valid"`
			Errors []ValidationError `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v body=%s", err, response.Body.String())
	}
	if result.Data.Valid != wantValid {
		t.Fatalf("valid=%v want=%v body=%s", result.Data.Valid, wantValid, response.Body.String())
	}
	if wantCode == "" {
		if len(result.Data.Errors) != 0 {
			t.Fatalf("unexpected errors=%#v", result.Data.Errors)
		}
		return
	}
	found := false
	for _, item := range result.Data.Errors {
		if string(item.Code) == wantCode {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing code %s in %#v", wantCode, result.Data.Errors)
	}
}

func assertHTTP(t *testing.T, router http.Handler, method, path string, body any, status int) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, status, response.Body.String())
	}
}
