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

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Mock: AuthService
// ---------------------------------------------------------------------------

type mockAuthService struct{ mock.Mock }

func (m *mockAuthService) Register(ctx context.Context, req dto.RegisterRequest) (*dto.TokenResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.TokenResponse), args.Error(1)
}

func (m *mockAuthService) Login(ctx context.Context, req dto.LoginRequest) (*dto.TokenResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.TokenResponse), args.Error(1)
}

func (m *mockAuthService) Refresh(ctx context.Context, refreshToken string) (*dto.AccessTokenResponse, error) {
	args := m.Called(ctx, refreshToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.AccessTokenResponse), args.Error(1)
}

func (m *mockAuthService) Logout(ctx context.Context, userID uint64) error {
	return m.Called(ctx, userID).Error(0)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newRouter(svc *mockAuthService) *gin.Engine {
	r := gin.New()
	r.Use(middleware.Error())
	h := handler.NewAuthHandler(svc)
	auth := r.Group("/api/v1/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)
	// /logout requires JWT — in tests we inject userID directly via a fake middleware
	auth.POST("/logout", fakeAuthMiddleware(42), h.Logout)
	return r
}

// fakeAuthMiddleware injects a fixed userID into the Gin context (test-only).
func fakeAuthMiddleware(userID uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, userID)
		c.Next()
	}
}

func postJSON(r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const testRefreshUUID = "00000000-0000-0000-0000-000000000000"

func fakeTokenResp() *dto.TokenResponse {
	return &dto.TokenResponse{
		AccessToken:  "at",
		RefreshToken: testRefreshUUID,
		TokenType:    "Bearer",
		ExpiresIn:    900,
		User:         dto.UserInfo{ID: 1, Email: "a@example.com", CreatedAt: time.Now()},
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestAuthHandler_Register_BadRequest(t *testing.T) {
	r := newRouter(new(mockAuthService))
	w := postJSON(r, "/api/v1/auth/register", map[string]string{"email": "not-an-email"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthHandler_Register_PasswordMismatch(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Register", mock.Anything, mock.Anything).Return(nil, apierrors.ErrPasswordMismatch)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/register", dto.RegisterRequest{
		Email: "a@example.com", Password: "12345678", PasswordConfirm: "different",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "PASSWORD_MISMATCH", body["code"])
}

func TestAuthHandler_Register_Success(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Register", mock.Anything, mock.Anything).Return(fakeTokenResp(), nil)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/register", dto.RegisterRequest{
		Email: "a@example.com", Password: "12345678", PasswordConfirm: "12345678",
	})
	assert.Equal(t, http.StatusCreated, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Bearer", body["token_type"])
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestAuthHandler_Login_BadRequest(t *testing.T) {
	r := newRouter(new(mockAuthService))
	w := postJSON(r, "/api/v1/auth/login", map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Login", mock.Anything, mock.Anything).Return(nil, apierrors.ErrInvalidCredentials)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/login", dto.LoginRequest{Email: "a@example.com", Password: "wrong"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthHandler_Login_Success(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Login", mock.Anything, mock.Anything).Return(fakeTokenResp(), nil)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/login", dto.LoginRequest{Email: "a@example.com", Password: "correct"})
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Refresh — no JWT required, token encodes user ID
// ---------------------------------------------------------------------------

func TestAuthHandler_Refresh_BadRequest(t *testing.T) {
	r := newRouter(new(mockAuthService))
	w := postJSON(r, "/api/v1/auth/refresh", map[string]string{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthHandler_Refresh_NotUUID_BadRequest(t *testing.T) {
	// Non-UUID input must be rejected at the binding layer (400), not service layer (401)
	r := newRouter(new(mockAuthService))
	w := postJSON(r, "/api/v1/auth/refresh", map[string]string{"refresh_token": "not-a-uuid"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthHandler_Refresh_ServiceInvalidToken(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Refresh", mock.Anything, testRefreshUUID).Return(nil, apierrors.ErrInvalidRefreshToken)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/refresh", dto.RefreshRequest{RefreshToken: testRefreshUUID})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "INVALID_REFRESH_TOKEN", body["code"])
}

func TestAuthHandler_Refresh_Success(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Refresh", mock.Anything, testRefreshUUID).
		Return(&dto.AccessTokenResponse{AccessToken: "new-at", TokenType: "Bearer", ExpiresIn: 900}, nil)

	r := newRouter(svc)
	w := postJSON(r, "/api/v1/auth/refresh", dto.RefreshRequest{RefreshToken: testRefreshUUID})
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Logout — JWT required (injected via fakeAuthMiddleware in test router)
// ---------------------------------------------------------------------------

func TestAuthHandler_Logout_Success(t *testing.T) {
	svc := new(mockAuthService)
	svc.On("Logout", mock.Anything, uint64(42)).Return(nil)

	r := newRouter(svc)
	// No body needed per spec
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	svc.AssertCalled(t, "Logout", mock.Anything, uint64(42))
}
