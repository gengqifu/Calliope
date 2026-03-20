package handler

import (
	"crypto/hmac"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/service"
	apierrors "github.com/calliope/api/pkg/errors"
)

// InternalHandler handles internal callbacks from the Python inference worker.
type InternalHandler struct {
	svc service.TaskService
}

// NewInternalHandler creates a new InternalHandler.
func NewInternalHandler(svc service.TaskService) *InternalHandler {
	return &InternalHandler{svc: svc}
}

// InternalAuth returns a middleware that validates the shared secret used by
// the Python inference worker. Enforces:
//  1. X-Timestamp header within ±60 s of server time (replay protection).
//  2. Authorization: Bearer <secret> via constant-time comparison (timing-attack safe).
func InternalAuth(secret string) gin.HandlerFunc {
	expected := []byte("Bearer " + secret)
	return func(c *gin.Context) {
		// Replay protection: reject requests whose timestamp is >60 s stale or future.
		tsStr := c.GetHeader("X-Timestamp")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil || absInt64(time.Now().Unix()-ts) > 60 {
			_ = c.Error(apierrors.ErrUnauthorized)
			c.Abort()
			return
		}

		if !hmac.Equal([]byte(c.GetHeader("Authorization")), expected) {
			_ = c.Error(apierrors.ErrUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}

// UpdateStatus handles POST /internal/tasks/:task_id/status.
func (h *InternalHandler) UpdateStatus(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": "task_id 必须为正整数"})
		return
	}

	var req dto.UpdateTaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}

	if err := h.svc.UpdateTaskStatus(c.Request.Context(), taskID, req); err != nil {
		var conflictErr *service.CallbackConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusConflict, gin.H{
				"code":           "CALLBACK_CONFLICT",
				"reason":         conflictErr.Reason,
				"current_status": conflictErr.CurrentStatus,
			})
			return
		}
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
