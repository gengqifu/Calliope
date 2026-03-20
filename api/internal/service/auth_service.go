package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/repository"
	apierrors "github.com/calliope/api/pkg/errors"
)

// AuthConfig holds all auth-related configuration needed by AuthService.
type AuthConfig struct {
	JWTSecret        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	RefreshTokenLong time.Duration
	MaxLoginAttempts int
	LockDuration     time.Duration
}

// RedisClient is the subset of Redis operations required by services.
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	Incr(ctx context.Context, key string) (int64, error)
	Decr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)
	// Eval executes a Lua script atomically.
	// keys and args correspond to KEYS and ARGV in the script.
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
	// Publish sends a message to a Pub/Sub channel.
	Publish(ctx context.Context, channel string, message interface{}) error
}

// AuthService defines the authentication business logic.
type AuthService interface {
	Register(ctx context.Context, req dto.RegisterRequest) (*dto.TokenResponse, error)
	Login(ctx context.Context, req dto.LoginRequest) (*dto.TokenResponse, error)
	// Refresh issues a new Access Token. refreshToken must be a valid UUID.
	Refresh(ctx context.Context, refreshToken string) (*dto.AccessTokenResponse, error)
	// Logout revokes the current user's refresh token. userID comes from the JWT (Auth middleware).
	Logout(ctx context.Context, userID uint64) error
}

type authServiceImpl struct {
	cfg  AuthConfig
	repo repository.UserRepository
	rdb  RedisClient
}

// NewAuthService creates a new AuthService.
func NewAuthService(cfg AuthConfig, repo repository.UserRepository, rdb RedisClient) AuthService {
	return &authServiceImpl{cfg: cfg, repo: repo, rdb: rdb}
}

// ---- Register ---------------------------------------------------------------

func (s *authServiceImpl) Register(ctx context.Context, req dto.RegisterRequest) (*dto.TokenResponse, error) {
	if req.Password != req.PasswordConfirm {
		return nil, apierrors.ErrPasswordMismatch
	}

	// Pre-check email uniqueness (best-effort; repo.Create handles the race via DB constraint)
	existing, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
		return nil, fmt.Errorf("authService.Register: %w", err)
	}
	if existing != nil {
		return nil, apierrors.ErrEmailAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, fmt.Errorf("authService.Register: bcrypt: %w", err)
	}

	user := &model.User{
		Email:    req.Email,
		Password: string(hash),
	}
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("authService.Register: %w", err)
	}

	return s.issueTokens(ctx, user, false)
}

// ---- Login ------------------------------------------------------------------

func (s *authServiceImpl) Login(ctx context.Context, req dto.LoginRequest) (*dto.TokenResponse, error) {
	locked, err := s.rdb.Exists(ctx, s.lockKey(req.Email))
	if err != nil {
		return nil, fmt.Errorf("authService.Login: check lock: %w", err)
	}
	if locked {
		return nil, apierrors.ErrAccountLocked
	}

	user, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, apierrors.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("authService.Login: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, s.recordFailure(ctx, req.Email)
	}

	_ = s.rdb.Del(ctx, s.failKey(req.Email))

	return s.issueTokens(ctx, user, req.RememberMe)
}

// ---- Refresh ----------------------------------------------------------------

// Refresh validates the UUID refresh token and issues a new Access Token.
//
// Redis key design (dual-key, single-device model):
//   - Canonical:    calliope:auth:refresh:{userID}        → {tokenUUID}
//   - Reverse:      calliope:auth:refresh:token:{uuid}    → {userID}
//
// The client-visible refresh token is always a bare UUID, per the API spec.
// Cross-checking both keys prevents use of a superseded token after re-login.
func (s *authServiceImpl) Refresh(ctx context.Context, refreshToken string) (*dto.AccessTokenResponse, error) {
	// Step 1: reverse lookup — find who this token belongs to
	userIDStr, err := s.rdb.Get(ctx, s.reverseKey(refreshToken))
	if err != nil || userIDStr == "" {
		return nil, apierrors.ErrInvalidRefreshToken
	}
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return nil, apierrors.ErrInvalidRefreshToken
	}

	// Step 2: cross-check — canonical key must still point to this UUID
	// (if the user has logged in again, the canonical key holds a newer token)
	storedUUID, err := s.rdb.Get(ctx, s.canonicalKey(userID))
	if err != nil || storedUUID != refreshToken {
		return nil, apierrors.ErrInvalidRefreshToken
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("authService.Refresh: %w", err)
	}

	accessToken, expiresIn, err := s.generateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("authService.Refresh: %w", err)
	}

	return &dto.AccessTokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
	}, nil
}

// ---- Logout -----------------------------------------------------------------

// Logout deletes the user's refresh token from Redis.
// Spec: DEL calliope:auth:refresh:{userID}
func (s *authServiceImpl) Logout(ctx context.Context, userID uint64) error {
	// Retrieve current UUID so we can clean up the reverse key too
	tokenUUID, _ := s.rdb.Get(ctx, s.canonicalKey(userID))

	if err := s.rdb.Del(ctx, s.canonicalKey(userID)); err != nil {
		return fmt.Errorf("authService.Logout: %w", err)
	}

	// Best-effort reverse key cleanup (non-fatal)
	if tokenUUID != "" {
		_ = s.rdb.Del(ctx, s.reverseKey(tokenUUID))
	}
	return nil
}

// ---- Private helpers --------------------------------------------------------

func (s *authServiceImpl) issueTokens(ctx context.Context, user *model.User, rememberMe bool) (*dto.TokenResponse, error) {
	accessToken, expiresIn, err := s.generateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("authService.issueTokens: %w", err)
	}

	refreshTTL := s.cfg.RefreshTokenTTL
	if rememberMe {
		refreshTTL = s.cfg.RefreshTokenLong
	}

	tokenUUID := uuid.NewString()

	// Clean up the previous reverse key before overwriting the canonical key.
	// Without this, every re-login would leave an orphaned reverse key that
	// persists until its TTL expires.
	if oldUUID, err := s.rdb.Get(ctx, s.canonicalKey(user.ID)); err == nil && oldUUID != "" {
		_ = s.rdb.Del(ctx, s.reverseKey(oldUUID))
	}

	// Canonical key: calliope:auth:refresh:{userID} → uuid
	if err := s.rdb.Set(ctx, s.canonicalKey(user.ID), tokenUUID, refreshTTL); err != nil {
		return nil, fmt.Errorf("authService.issueTokens: canonical key: %w", err)
	}
	// Reverse key: calliope:auth:refresh:token:{uuid} → userID
	if err := s.rdb.Set(ctx, s.reverseKey(tokenUUID), fmt.Sprintf("%d", user.ID), refreshTTL); err != nil {
		return nil, fmt.Errorf("authService.issueTokens: reverse key: %w", err)
	}

	return &dto.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: tokenUUID, // bare UUID — spec-compliant
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		User: dto.UserInfo{
			ID:        user.ID,
			Email:     user.Email,
			Nickname:  user.Nickname,
			CreatedAt: user.CreatedAt,
		},
	}, nil
}

type jwtClaims struct {
	UserID uint64 `json:"uid"`
	jwt.RegisteredClaims
}

func (s *authServiceImpl) generateAccessToken(user *model.User) (string, int64, error) {
	expiresIn := int64(s.cfg.AccessTokenTTL.Seconds())
	claims := jwtClaims{
		UserID: user.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.cfg.AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   fmt.Sprintf("%d", user.ID),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return "", 0, fmt.Errorf("generateAccessToken: %w", err)
	}
	return signed, expiresIn, nil
}

func (s *authServiceImpl) recordFailure(ctx context.Context, email string) error {
	failKey := s.failKey(email)
	count, err := s.rdb.Incr(ctx, failKey)
	if err != nil {
		return fmt.Errorf("authService.recordFailure: %w", err)
	}
	_ = s.rdb.Expire(ctx, failKey, s.cfg.LockDuration)

	if count >= int64(s.cfg.MaxLoginAttempts) {
		_ = s.rdb.Set(ctx, s.lockKey(email), "1", s.cfg.LockDuration)
		return apierrors.ErrAccountLocked
	}
	return apierrors.ErrInvalidCredentials
}

// canonicalKey returns calliope:auth:refresh:{userID} (per-user, used for logout)
func (s *authServiceImpl) canonicalKey(userID uint64) string {
	return fmt.Sprintf("calliope:auth:refresh:%d", userID)
}

// reverseKey returns calliope:auth:refresh:token:{uuid} (for refresh lookup without JWT)
func (s *authServiceImpl) reverseKey(tokenUUID string) string {
	return fmt.Sprintf("calliope:auth:refresh:token:%s", tokenUUID)
}

func (s *authServiceImpl) lockKey(email string) string {
	return fmt.Sprintf("calliope:auth:lock:%s", email)
}

func (s *authServiceImpl) failKey(email string) string {
	return fmt.Sprintf("calliope:auth:fail:%s", email)
}
