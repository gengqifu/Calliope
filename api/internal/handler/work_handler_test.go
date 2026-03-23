package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
// Mock: WorkService
// ---------------------------------------------------------------------------

type mockWorkService struct{ mock.Mock }

func (m *mockWorkService) SaveWork(ctx context.Context, userID uint64, req dto.SaveWorkRequest) (*dto.WorkResponse, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.WorkResponse), args.Error(1)
}

func (m *mockWorkService) GetWork(ctx context.Context, userID, workID uint64) (*dto.WorkResponse, error) {
	args := m.Called(ctx, userID, workID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.WorkResponse), args.Error(1)
}

func (m *mockWorkService) ListWorks(ctx context.Context, userID uint64, req dto.ListWorksRequest) (*dto.ListWorksResponse, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.ListWorksResponse), args.Error(1)
}

func (m *mockWorkService) GetDownloadURL(ctx context.Context, userID, workID uint64) (*dto.DownloadURLResponse, error) {
	args := m.Called(ctx, userID, workID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.DownloadURLResponse), args.Error(1)
}

func (m *mockWorkService) DeleteWork(ctx context.Context, userID, workID uint64) error {
	return m.Called(ctx, userID, workID).Error(0)
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func setupWorkRouter(svc *mockWorkService, userID uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Error())
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, userID)
		c.Next()
	})
	h := handler.NewWorkHandler(svc)
	r.POST("/works", h.Save)
	r.GET("/works", h.List)
	r.GET("/works/:work_id", h.Get)
	r.GET("/works/:work_id/download", h.GetDownloadURL)
	r.DELETE("/works/:work_id", h.Delete)
	return r
}

func sampleWorkResponse() *dto.WorkResponse {
	return &dto.WorkResponse{
		ID:                88,
		Title:             "My Song",
		Prompt:            "test prompt",
		Mode:              "vocal",
		AudioURL:          "https://cdn/x",
		AudioURLExpiresAt: time.Now().Add(time.Hour),
		DurationSeconds:   30,
		CreatedAt:         time.Now(),
	}
}

// ---------------------------------------------------------------------------
// POST /works tests
// ---------------------------------------------------------------------------

func TestWorkHandler_Save_Success(t *testing.T) {
	svc := new(mockWorkService)
	req := dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "My Song"}
	svc.On("SaveWork", mock.Anything, uint64(1), req).Return(sampleWorkResponse(), nil)

	r := setupWorkRouter(svc, 1)
	body := bodyJSON(req)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodPost, "/works", body)
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp dto.WorkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, uint64(88), resp.ID)
}

func TestWorkHandler_Save_ValidationError(t *testing.T) {
	r := setupWorkRouter(new(mockWorkService), 1)
	// Missing required fields
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodPost, "/works", strings.NewReader(`{}`))
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkHandler_Save_WorkAlreadySaved(t *testing.T) {
	svc := new(mockWorkService)
	req := dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "My Song"}
	svc.On("SaveWork", mock.Anything, uint64(1), req).Return(nil, apierrors.ErrWorkAlreadySaved)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodPost, "/works", bodyJSON(req))
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestWorkHandler_Save_TaskNotCompleted(t *testing.T) {
	svc := new(mockWorkService)
	req := dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "My Song"}
	svc.On("SaveWork", mock.Anything, uint64(1), req).Return(nil, apierrors.ErrTaskNotCompleted)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodPost, "/works", bodyJSON(req))
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// GET /works/:work_id tests
// ---------------------------------------------------------------------------

func TestWorkHandler_Get_Success(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("GetWork", mock.Anything, uint64(1), uint64(88)).Return(sampleWorkResponse(), nil)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodGet, "/works/88", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWorkHandler_Get_NotFound(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("GetWork", mock.Anything, uint64(1), uint64(999)).Return(nil, apierrors.ErrNotFound)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodGet, "/works/999", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkHandler_Get_InvalidID(t *testing.T) {
	r := setupWorkRouter(new(mockWorkService), 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodGet, "/works/abc", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// GET /works tests
// ---------------------------------------------------------------------------

func TestWorkHandler_List_Success(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("ListWorks", mock.Anything, uint64(1), dto.ListWorksRequest{Page: 1, PageSize: 20}).
		Return(&dto.ListWorksResponse{
			Total:    1,
			Page:     1,
			PageSize: 20,
			Items:    []dto.WorkResponse{*sampleWorkResponse()},
		}, nil)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodGet, "/works?page=1&page_size=20", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp dto.ListWorksResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(1), resp.Total)
}

// ---------------------------------------------------------------------------
// GET /works/:work_id/download tests
// ---------------------------------------------------------------------------

func TestWorkHandler_GetDownloadURL_Success(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("GetDownloadURL", mock.Anything, uint64(1), uint64(88)).Return(&dto.DownloadURLResponse{
		DownloadURL: "https://cdn/x?attname=My+Song.mp3",
		Filename:    "My Song.mp3",
		ExpiresIn:   3600,
	}, nil)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodGet, "/works/88/download", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp dto.DownloadURLResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "My Song.mp3", resp.Filename)
}

// ---------------------------------------------------------------------------
// DELETE /works/:work_id tests
// ---------------------------------------------------------------------------

func TestWorkHandler_Delete_Success(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("DeleteWork", mock.Anything, uint64(1), uint64(88)).Return(nil)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodDelete, "/works/88", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestWorkHandler_Delete_Forbidden(t *testing.T) {
	svc := new(mockWorkService)
	svc.On("DeleteWork", mock.Anything, uint64(1), uint64(88)).Return(apierrors.ErrForbidden)

	r := setupWorkRouter(svc, 1)
	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest(http.MethodDelete, "/works/88", nil)
	r.ServeHTTP(w, httpReq)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
