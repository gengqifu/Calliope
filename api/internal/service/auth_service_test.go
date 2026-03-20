package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/service"
	apierrors "github.com/calliope/api/pkg/errors"
)

// ---------------------------------------------------------------------------
// Mock: UserRepository
// ---------------------------------------------------------------------------

type mockUserRepo struct{ mock.Mock }

func (m *mockUserRepo) Create(ctx context.Context, user *model.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockUserRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.User), args.Error(1)
}

func (m *mockUserRepo) FindByID(ctx context.Context, id uint64) (*model.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.User), args.Error(1)
}

// ---------------------------------------------------------------------------
// Mock: RedisClient
// ---------------------------------------------------------------------------

type mockRedis struct{ mock.Mock }

func (m *mockRedis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return m.Called(ctx, key, value, ttl).Error(0)
}

func (m *mockRedis) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *mockRedis) Del(ctx context.Context, keys ...string) error {
	callArgs := []interface{}{ctx}
	for _, k := range keys {
		callArgs = append(callArgs, k)
	}
	return m.Called(callArgs...).Error(0)
}

func (m *mockRedis) Incr(ctx context.Context, key string) (int64, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockRedis) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return m.Called(ctx, key, ttl).Error(0)
}

func (m *mockRedis) Exists(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestService(repo *mockUserRepo, rdb *mockRedis) service.AuthService {
	cfg := service.AuthConfig{
		JWTSecret:        "test-secret-key-at-least-32-chars!",
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  7 * 24 * time.Hour,
		RefreshTokenLong: 30 * 24 * time.Hour,
		MaxLoginAttempts: 5,
		LockDuration:     15 * time.Minute,
	}
	return service.NewAuthService(cfg, repo, rdb)
}

func hashedPassword(t *testing.T, plain string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	require.NoError(t, err)
	return string(h)
}

// ---------------------------------------------------------------------------
// Register tests
// ---------------------------------------------------------------------------

func TestAuthService_Register_PasswordMismatch(t *testing.T) {
	svc := newTestService(new(mockUserRepo), new(mockRedis))
	_, err := svc.Register(context.Background(), dto.RegisterRequest{
		Email:           "a@example.com",
		Password:        "password1",
		PasswordConfirm: "password2",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrPasswordMismatch)
}

func TestAuthService_Register_EmailAlreadyExists(t *testing.T) {
	repo := new(mockUserRepo)
	repo.On("FindByEmail", mock.Anything, "taken@example.com").
		Return(&model.User{ID: 1, Email: "taken@example.com"}, nil)

	svc := newTestService(repo, new(mockRedis))
	_, err := svc.Register(context.Background(), dto.RegisterRequest{
		Email:           "taken@example.com",
		Password:        "password1",
		PasswordConfirm: "password1",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrEmailAlreadyExists)
}

func TestAuthService_Register_Success(t *testing.T) {
	repo := new(mockUserRepo)
	rdb := new(mockRedis)

	repo.On("FindByEmail", mock.Anything, "new@example.com").Return(nil, apierrors.ErrNotFound)
	repo.On("Create", mock.Anything, mock.AnythingOfType("*model.User")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.User).ID = 42
		})
	// First registration: no previous session
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:42").Return("", nil)
	// Canonical key: calliope:auth:refresh:42 → uuid
	rdb.On("Set", mock.Anything, "calliope:auth:refresh:42", mock.AnythingOfType("string"), mock.AnythingOfType("time.Duration")).Return(nil)
	// Reverse key: calliope:auth:refresh:token:{uuid} → "42"
	rdb.On("Set", mock.Anything, mock.MatchedBy(func(k string) bool {
		return len(k) > len("calliope:auth:refresh:token:")
	}), "42", mock.AnythingOfType("time.Duration")).Return(nil)

	svc := newTestService(repo, rdb)
	resp, err := svc.Register(context.Background(), dto.RegisterRequest{
		Email: "new@example.com", Password: "password1", PasswordConfirm: "password1",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	// refresh_token must be a bare UUID (spec-compliant)
	assert.Len(t, resp.RefreshToken, 36, "refresh_token should be a UUID")
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, uint64(42), resp.User.ID)
}

// ---------------------------------------------------------------------------
// Login tests
// ---------------------------------------------------------------------------

func TestAuthService_Login_AccountLocked(t *testing.T) {
	rdb := new(mockRedis)
	rdb.On("Exists", mock.Anything, mock.Anything).Return(true, nil)

	svc := newTestService(new(mockUserRepo), rdb)
	_, err := svc.Login(context.Background(), dto.LoginRequest{Email: "locked@example.com", Password: "any"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrAccountLocked)
}

func TestAuthService_Login_InvalidCredentials_WrongPassword(t *testing.T) {
	repo := new(mockUserRepo)
	rdb := new(mockRedis)
	rdb.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
	repo.On("FindByEmail", mock.Anything, "user@example.com").
		Return(&model.User{ID: 1, Email: "user@example.com", Password: hashedPassword(t, "correct")}, nil)
	rdb.On("Incr", mock.Anything, mock.Anything).Return(int64(1), nil)
	rdb.On("Expire", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	svc := newTestService(repo, rdb)
	_, err := svc.Login(context.Background(), dto.LoginRequest{Email: "user@example.com", Password: "wrong"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrInvalidCredentials)
}

func TestAuthService_Login_LocksAfterMaxAttempts(t *testing.T) {
	repo := new(mockUserRepo)
	rdb := new(mockRedis)
	rdb.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
	repo.On("FindByEmail", mock.Anything, "user@example.com").
		Return(&model.User{ID: 1, Email: "user@example.com", Password: hashedPassword(t, "correct")}, nil)
	rdb.On("Incr", mock.Anything, mock.Anything).Return(int64(5), nil)
	rdb.On("Expire", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	rdb.On("Set", mock.Anything, mock.Anything, "1", mock.AnythingOfType("time.Duration")).Return(nil)

	svc := newTestService(repo, rdb)
	_, err := svc.Login(context.Background(), dto.LoginRequest{Email: "user@example.com", Password: "wrong"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrAccountLocked)
}

func TestAuthService_Login_CleansUpOldReverseKey(t *testing.T) {
	// When a user logs in again, the old reverse key must be deleted before
	// the new canonical key is written.
	repo := new(mockUserRepo)
	rdb := new(mockRedis)

	oldUUID := "old-uuid-value"
	pw := hashedPassword(t, "correct")

	rdb.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
	repo.On("FindByEmail", mock.Anything, "user@example.com").
		Return(&model.User{ID: 1, Email: "user@example.com", Password: pw, Status: 1}, nil)
	rdb.On("Del", mock.Anything, "calliope:auth:fail:user@example.com").Return(nil)

	// Canonical key already holds the old UUID from a previous login
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:1").Return(oldUUID, nil)
	// Old reverse key must be deleted
	rdb.On("Del", mock.Anything, "calliope:auth:refresh:token:"+oldUUID).Return(nil)

	// New keys written
	rdb.On("Set", mock.Anything, "calliope:auth:refresh:1", mock.AnythingOfType("string"), mock.AnythingOfType("time.Duration")).Return(nil)
	rdb.On("Set", mock.Anything, mock.MatchedBy(func(k string) bool {
		return len(k) > len("calliope:auth:refresh:token:")
	}), "1", mock.AnythingOfType("time.Duration")).Return(nil)

	svc := newTestService(repo, rdb)
	_, err := svc.Login(context.Background(), dto.LoginRequest{Email: "user@example.com", Password: "correct"})
	require.NoError(t, err)

	rdb.AssertCalled(t, "Del", mock.Anything, "calliope:auth:refresh:token:"+oldUUID)
}

func TestAuthService_Login_Success(t *testing.T) {
	repo := new(mockUserRepo)
	rdb := new(mockRedis)
	pw := hashedPassword(t, "correct")
	rdb.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
	repo.On("FindByEmail", mock.Anything, "user@example.com").
		Return(&model.User{ID: 1, Email: "user@example.com", Password: pw, Status: 1}, nil)
	rdb.On("Del", mock.Anything, mock.Anything).Return(nil)
	// First login: canonical key is empty (no previous session)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:1").Return("", nil)
	rdb.On("Set", mock.Anything, "calliope:auth:refresh:1", mock.AnythingOfType("string"), mock.AnythingOfType("time.Duration")).Return(nil)
	rdb.On("Set", mock.Anything, mock.MatchedBy(func(k string) bool {
		return len(k) > len("calliope:auth:refresh:token:")
	}), "1", mock.AnythingOfType("time.Duration")).Return(nil)

	svc := newTestService(repo, rdb)
	resp, err := svc.Login(context.Background(), dto.LoginRequest{Email: "user@example.com", Password: "correct"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Len(t, resp.RefreshToken, 36, "refresh_token should be a UUID")
}

// ---------------------------------------------------------------------------
// Refresh tests
// ---------------------------------------------------------------------------

const testUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestAuthService_Refresh_TokenNotInRedis(t *testing.T) {
	rdb := new(mockRedis)
	// Reverse key not found
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:token:"+testUUID).Return("", apierrors.ErrInvalidRefreshToken)

	svc := newTestService(new(mockUserRepo), rdb)
	_, err := svc.Refresh(context.Background(), testUUID)
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrInvalidRefreshToken)
}

func TestAuthService_Refresh_CanonicalMismatch(t *testing.T) {
	// Reverse key exists but canonical key holds a different (newer) UUID
	rdb := new(mockRedis)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:token:"+testUUID).Return("1", nil)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:1").Return("other-uuid", nil)

	svc := newTestService(new(mockUserRepo), rdb)
	_, err := svc.Refresh(context.Background(), testUUID)
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrInvalidRefreshToken)
}

func TestAuthService_Refresh_Success(t *testing.T) {
	repo := new(mockUserRepo)
	rdb := new(mockRedis)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:token:"+testUUID).Return("1", nil)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:1").Return(testUUID, nil)
	repo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.User{ID: 1, Email: "user@example.com", Status: 1}, nil)

	svc := newTestService(repo, rdb)
	resp, err := svc.Refresh(context.Background(), testUUID)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
}

// ---------------------------------------------------------------------------
// Logout tests
// ---------------------------------------------------------------------------

func TestAuthService_Logout_Success(t *testing.T) {
	rdb := new(mockRedis)
	// Get canonical key to find current UUID (for reverse key cleanup)
	rdb.On("Get", mock.Anything, "calliope:auth:refresh:1").Return(testUUID, nil)
	// DEL canonical key
	rdb.On("Del", mock.Anything, "calliope:auth:refresh:1").Return(nil)
	// DEL reverse key (best-effort)
	rdb.On("Del", mock.Anything, "calliope:auth:refresh:token:"+testUUID).Return(nil)

	svc := newTestService(new(mockUserRepo), rdb)
	err := svc.Logout(context.Background(), 1)
	require.NoError(t, err)
	rdb.AssertCalled(t, "Del", mock.Anything, "calliope:auth:refresh:1")
}
