package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/handler"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/service"
	apierrors "github.com/calliope/api/pkg/errors"
)

const testInternalSecret = "test-internal-secret-abc123"

func nowTS() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func setupInternalRouter(svc *mockTaskService) *gin.Engine {
	r := gin.New()
	r.Use(middleware.Error())
	internal := r.Group("/internal", handler.InternalAuth(testInternalSecret))
	h := handler.NewInternalHandler(svc)
	internal.POST("/tasks/:task_id/status", h.UpdateStatus)
	return r
}

func TestInternalHandler_UpdateStatus_Unauthorized_WrongSecret(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer wrong-secret")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalHandler_UpdateStatus_Unauthorized_ExpiredTimestamp(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	// timestamp 120 seconds in the past — outside ±60 s window
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", time.Now().Unix()-120))
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalHandler_UpdateStatus_Unauthorized_MissingTimestamp(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	// No X-Timestamp header at all
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalHandler_UpdateStatus_InvalidStatus(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "unknown"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInternalHandler_UpdateStatus_Processing_Success(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("UpdateTaskStatus", mock.Anything, uint64(1), dto.UpdateTaskStatusRequest{Status: "processing"}).
		Return(nil)

	r := setupInternalRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	svc.AssertExpectations(t)
}

func TestInternalHandler_UpdateStatus_NotFound(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("UpdateTaskStatus", mock.Anything, uint64(999), mock.Anything).
		Return(apierrors.ErrNotFound)

	r := setupInternalRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/999/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestInternalHandler_UpdateStatus_Conflict_Duplicate(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("UpdateTaskStatus", mock.Anything, uint64(1), mock.Anything).
		Return(&service.CallbackConflictError{Reason: "duplicate", CurrentStatus: "processing"})

	r := setupInternalRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "processing"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "CALLBACK_CONFLICT", body["code"])
	assert.Equal(t, "duplicate", body["reason"])
	assert.Equal(t, "processing", body["current_status"])
}

func TestInternalHandler_UpdateStatus_Completed_MissingCandidateKey(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	// candidate_b_key and duration_seconds missing
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(map[string]interface{}{
			"status":        "completed",
			"candidate_a_key": "audio/1/a.mp3",
		}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "VALIDATION_ERROR", body["code"])
}

func TestInternalHandler_UpdateStatus_Completed_MissingDuration(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(map[string]interface{}{
			"status":          "completed",
			"candidate_a_key": "audio/1/a.mp3",
			"candidate_b_key": "audio/1/b.mp3",
			// duration_seconds missing
		}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInternalHandler_UpdateStatus_Failed_MissingFailReason(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(map[string]interface{}{"status": "failed"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInternalHandler_UpdateStatus_Failed_InvalidFailReason(t *testing.T) {
	r := setupInternalRouter(new(mockTaskService))

	w := httptest.NewRecorder()
	reason := "unknown_reason"
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(map[string]interface{}{"status": "failed", "fail_reason": reason}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "VALIDATION_ERROR", body["code"])
}

func TestInternalHandler_UpdateStatus_Conflict_InvalidTransition(t *testing.T) {
	svc := new(mockTaskService)
	svc.On("UpdateTaskStatus", mock.Anything, uint64(1), mock.Anything).
		Return(&service.CallbackConflictError{Reason: "invalid_transition", CurrentStatus: "completed"})

	r := setupInternalRouter(svc)
	w := httptest.NewRecorder()
	reason := "timeout"
	req := httptest.NewRequest(http.MethodPost, "/internal/tasks/1/status",
		bodyJSON(dto.UpdateTaskStatusRequest{Status: "failed", FailReason: &reason}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", nowTS())
	req.Header.Set("Authorization", "Bearer "+testInternalSecret)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "CALLBACK_CONFLICT", body["code"])
	assert.Equal(t, "invalid_transition", body["reason"])
	assert.Equal(t, "completed", body["current_status"])
}
