package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/service"
)

// AuthHandler handles HTTP requests for the /auth group.
type AuthHandler struct {
	svc service.AuthService
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(svc service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}

	resp, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}

	resp, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Refresh handles POST /auth/refresh.
// No JWT required: the refresh token encodes the user ID internally.
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req dto.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}

	resp, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Logout handles POST /auth/logout.
// Requires JWT (Auth middleware must be applied at the router level).
// No request body needed: user ID is extracted from the JWT.
func (h *AuthHandler) Logout(c *gin.Context) {
	userID, _ := c.Get(middleware.ContextKeyUserID)
	uid, _ := userID.(uint64)

	if err := h.svc.Logout(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}
