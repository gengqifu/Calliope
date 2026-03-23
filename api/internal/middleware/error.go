package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	apierrors "github.com/calliope/api/pkg/errors"
)

// errorCodeToStatus maps AppError codes to HTTP status codes.
var errorCodeToStatus = map[string]int{
	"NOT_FOUND":              http.StatusNotFound,
	"UNAUTHORIZED":           http.StatusUnauthorized,
	"FORBIDDEN":              http.StatusForbidden,
	"INTERNAL_ERROR":         http.StatusInternalServerError,
	"PASSWORD_MISMATCH":      http.StatusBadRequest,
	"EMAIL_ALREADY_EXISTS":   http.StatusConflict,
	"INVALID_CREDENTIALS":    http.StatusUnauthorized,
	"ACCOUNT_LOCKED":         http.StatusForbidden,
	"INVALID_REFRESH_TOKEN":  http.StatusUnauthorized,
	// Task-specific
	"INSUFFICIENT_CREDITS":   http.StatusPaymentRequired,
	"QUEUE_FULL":             http.StatusTooManyRequests,
	"CONTENT_FILTERED":       http.StatusBadRequest,
	"TASK_NOT_COMPLETED":        http.StatusBadRequest,
	"INVALID_STATUS_TRANSITION": http.StatusConflict,
	// Work-specific
	"WORK_ALREADY_SAVED": http.StatusConflict,
}

// Error returns a Gin middleware that converts errors attached via c.Error()
// into a uniform JSON response: {"code": "...", "message": "..."}.
func Error() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		err := c.Errors.Last().Err

		var appErr *apierrors.AppError
		if errors.As(err, &appErr) {
			status, ok := errorCodeToStatus[appErr.Code()]
			if !ok {
				status = http.StatusInternalServerError
			}
			c.JSON(status, gin.H{
				"code":    appErr.Code(),
				"message": appErr.Message(),
			})
			return
		}

		// Unknown error — do not leak internals
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "INTERNAL_ERROR",
			"message": "服务器内部错误",
		})
	}
}
