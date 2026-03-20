package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/service"
)

// TaskHandler handles HTTP requests for the /tasks group.
type TaskHandler struct {
	svc service.TaskService
}

// NewTaskHandler creates a new TaskHandler.
func NewTaskHandler(svc service.TaskService) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// Create handles POST /tasks.
func (h *TaskHandler) Create(c *gin.Context) {
	var req dto.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.CreateTask(c.Request.Context(), userID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, resp)
}

// Get handles GET /tasks/:task_id.
func (h *TaskHandler) Get(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": "task_id 必须为正整数"})
		return
	}

	userID := c.MustGet(middleware.ContextKeyUserID).(uint64)

	resp, err := h.svc.GetTask(c.Request.Context(), userID, taskID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
