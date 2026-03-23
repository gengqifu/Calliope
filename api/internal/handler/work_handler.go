package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/service"
)

// WorkHandler handles HTTP requests for work management endpoints.
type WorkHandler struct {
	svc service.WorkService
}

// NewWorkHandler creates a new WorkHandler.
func NewWorkHandler(svc service.WorkService) *WorkHandler {
	return &WorkHandler{svc: svc}
}

// Save handles POST /api/v1/works — saves a selected candidate as a permanent work.
func (h *WorkHandler) Save(c *gin.Context) {
	var req dto.SaveWorkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": err.Error(),
		})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.SaveWork(c.Request.Context(), userID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// Get handles GET /api/v1/works/:work_id — returns a single work with a signed URL.
func (h *WorkHandler) Get(c *gin.Context) {
	workID, err := parseWorkID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": "work_id 格式无效",
		})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.GetWork(c.Request.Context(), userID, workID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// List handles GET /api/v1/works — returns a paginated list of the user's works.
func (h *WorkHandler) List(c *gin.Context) {
	var req dto.ListWorksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": err.Error(),
		})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.ListWorks(c.Request.Context(), userID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDownloadURL handles GET /api/v1/works/:work_id/download — returns a download URL.
func (h *WorkHandler) GetDownloadURL(c *gin.Context) {
	workID, err := parseWorkID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": "work_id 格式无效",
		})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.GetDownloadURL(c.Request.Context(), userID, workID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Delete handles DELETE /api/v1/works/:work_id — soft-deletes a work.
func (h *WorkHandler) Delete(c *gin.Context) {
	workID, err := parseWorkID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "VALIDATION_ERROR",
			"message": "work_id 格式无效",
		})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	if err := h.svc.DeleteWork(c.Request.Context(), userID, workID); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// parseWorkID extracts and validates the :work_id path parameter.
func parseWorkID(c *gin.Context) (uint64, error) {
	return strconv.ParseUint(c.Param("work_id"), 10, 64)
}
