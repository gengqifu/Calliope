package dto

import "time"

// RegisterRequest is the payload for POST /auth/register.
type RegisterRequest struct {
	Email           string `json:"email"            binding:"required,email,max=100"`
	Password        string `json:"password"         binding:"required,min=8,max=32"`
	PasswordConfirm string `json:"password_confirm" binding:"required"`
}

// LoginRequest is the payload for POST /auth/login.
type LoginRequest struct {
	Email      string `json:"email"       binding:"required,email"`
	Password   string `json:"password"    binding:"required"`
	RememberMe bool   `json:"remember_me"`
}

// RefreshRequest is the payload for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,uuid"`
}

// UserInfo is the user object embedded in token responses.
type UserInfo struct {
	ID        uint64    `json:"id"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	CreatedAt time.Time `json:"created_at"`
}

// TokenResponse is returned after a successful register or login.
type TokenResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	TokenType    string   `json:"token_type"`
	ExpiresIn    int64    `json:"expires_in"` // seconds
	User         UserInfo `json:"user"`
}

// AccessTokenResponse is returned after a successful refresh.
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}
