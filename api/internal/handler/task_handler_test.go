package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/handler"
	"github.com/calliope/api/internal/middleware"
	apierrors "github.com/calliope/api/pkg/errors"
)

// ---------------------------------------------------------------------------
// Mock: TaskService
// ---------------------------------------------------------------------------

type mockTaskService struct{ mock.Mock }

func (m *mockTaskService) CreateTask(ctx context.Context, userID uint64, req dto.CreateTaskRequest) (*dto.CreateTaskResponse, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.CreateTaskResponse), args.Error(1)
}

func (m *mockTaskService) GetTask(ctx context.Context, userID uint64, taskID uint64) (*dto.TaskDetailResponse, error) {
	args := m.Called(ctx, userID, taskID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.TaskDetailResponse), args.Error(1)
}

func (m *mockTaskService) UpdateTaskStatus(ctx context.Context, taskID uint64, req dto.UpdateTaskStatusRequest) error {
	return m.Called(ctx, taskID, req).Error(0)
}

func (m *mockTaskService) FixTimedOutTasks(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupTaskRouter builds a Gin engine with the task routes and injects userID
// via context (simulating the Auth middleware).
func setupTaskRouter(svc *mockTaskService, userID uint64) *gin.Engine {
	r := gin.New()
	r.Use(middleware.Error())
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, userID)
		c.Next()
	})
	h := handler.NewTaskHandler(svc)
	r.POST("/tasks", h.Create)
	r.GET("/tasks/:task_id", h.Get)
	return r
}

func bodyJSON(v interface{}) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// ---------------------------------------------------------------------------
// POST /tasks tests
// ---------------------------------------------------------------------------

func TestTaskHandler_Create_ValidationError(t *testing.T) {
	r := setupTaskRouter(new(mockTaskService), 1)

	// Missing required field "prompt"
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bodyJSON(map[string]string{}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "VALIDATION_ERROR", body["code"])
}

func TestTaskHandler_Create_QueueFull(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("CreateTask", mock.Anything, uint64(1), mock.Anything).
		Return(nil, apierrors.ErrQueueFull)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bodyJSON(dto.CreateTaskRequest{Prompt: "test"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestTaskHandler_Create_InsufficientCredits(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("CreateTask", mock.Anything, uint64(1), mock.Anything).
		Return(nil, apierrors.ErrInsufficientCredits)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bodyJSON(dto.CreateTaskRequest{Prompt: "test"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
}

func TestTaskHandler_Create_Success(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("CreateTask", mock.Anything, uint64(1), mock.Anything).
		Return(&dto.CreateTaskResponse{TaskID: 42, Status: "queued", QueuePosition: 2, Message: "任务已提交，前方还有 2 个任务"}, nil)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bodyJSON(dto.CreateTaskRequest{Prompt: "一首欢快的歌"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var body dto.CreateTaskResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, uint64(42), body.TaskID)
	assert.Equal(t, "queued", body.Status)
}

// ---------------------------------------------------------------------------
// GET /tasks/:task_id tests
// ---------------------------------------------------------------------------

func TestTaskHandler_Get_InvalidID(t *testing.T) {
	r := setupTaskRouter(new(mockTaskService), 1)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/abc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTaskHandler_Get_NotFound(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("GetTask", mock.Anything, uint64(1), uint64(99)).
		Return(nil, apierrors.ErrNotFound)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/99", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTaskHandler_Get_Forbidden(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("GetTask", mock.Anything, uint64(1), uint64(5)).
		Return(nil, apierrors.ErrForbidden)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/5", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTaskHandler_Get_Success(t *testing.T) {
	svc := new(mockTaskService)
	pos := 1
	svc.On("GetTask", mock.Anything, uint64(1), uint64(42)).
		Return(&dto.TaskDetailResponse{
			TaskID:        42,
			Status:        "queued",
			Prompt:        "test",
			Mode:          "vocal",
			QueuePosition: &pos,
			CreatedAt:     time.Now(),
		}, nil)

	r := setupTaskRouter(svc, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks/42", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body dto.TaskDetailResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, uint64(42), body.TaskID)
	assert.Equal(t, "queued", body.Status)
}
