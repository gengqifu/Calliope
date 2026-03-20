package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/service"
	apierrors "github.com/calliope/api/pkg/errors"
)

// ---------------------------------------------------------------------------
// Mock: TaskRepository
// ---------------------------------------------------------------------------

type mockTaskRepo struct{ mock.Mock }

func (m *mockTaskRepo) Create(ctx context.Context, task *model.Task) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *mockTaskRepo) FindByID(ctx context.Context, id uint64) (*model.Task, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Task), args.Error(1)
}

func (m *mockTaskRepo) UpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}) error {
	return m.Called(ctx, id, fields).Error(0)
}

func (m *mockTaskRepo) ConditionalUpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}, fromStatuses []string) (bool, error) {
	args := m.Called(ctx, id, fields, fromStatuses)
	return args.Bool(0), args.Error(1)
}

func (m *mockTaskRepo) FindTimedOutProcessing(ctx context.Context, cutoff time.Time) ([]*model.Task, error) {
	args := m.Called(ctx, cutoff)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*model.Task), args.Error(1)
}

// ---------------------------------------------------------------------------
// Mock: CreditRepository
// ---------------------------------------------------------------------------

type mockCreditRepo struct{ mock.Mock }

func (m *mockCreditRepo) TryDeduct(ctx context.Context, userID uint64, date string) error {
	return m.Called(ctx, userID, date).Error(0)
}

func (m *mockCreditRepo) Refund(ctx context.Context, userID uint64, date string) error {
	return m.Called(ctx, userID, date).Error(0)
}

// ---------------------------------------------------------------------------
// Mock: OSSSignerClient
// ---------------------------------------------------------------------------

type mockOSSClient struct{ mock.Mock }

func (m *mockOSSClient) SignURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	args := m.Called(ctx, key, ttl)
	return args.String(0), args.Error(1)
}

// ---------------------------------------------------------------------------
// Helper: newTestTaskService
// ---------------------------------------------------------------------------

func newTestTaskService(
	taskRepo *mockTaskRepo,
	creditRepo *mockCreditRepo,
	rdb *mockRedis,
	oss *mockOSSClient,
) service.TaskService {
	cfg := service.TaskServiceConfig{
		QueueDepthMax:        20,
		TaskTimeoutSec:       180,
		SignedURLTTL:         time.Hour,
		ExpectedInferenceSec: 120,
	}
	return service.NewTaskService(cfg, taskRepo, creditRepo, rdb, oss)
}

// ---------------------------------------------------------------------------
// CreateTask tests
// ---------------------------------------------------------------------------

func TestTaskService_CreateTask_QueueFull(t *testing.T) {
	rdb := new(mockRedis)
	// queue depth >= 20
	rdb.On("Get", mock.Anything, "calliope:queue:depth").Return("20", nil)

	svc := newTestTaskService(new(mockTaskRepo), new(mockCreditRepo), rdb, new(mockOSSClient))
	_, err := svc.CreateTask(context.Background(), 1, dto.CreateTaskRequest{Prompt: "test"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrQueueFull)
}

func TestTaskService_CreateTask_InsufficientCredits(t *testing.T) {
	rdb := new(mockRedis)
	creditRepo := new(mockCreditRepo)

	rdb.On("Get", mock.Anything, "calliope:queue:depth").Return("0", nil)
	creditRepo.On("TryDeduct", mock.Anything, uint64(1), mock.AnythingOfType("string")).
		Return(apierrors.ErrInsufficientCredits)

	svc := newTestTaskService(new(mockTaskRepo), creditRepo, rdb, new(mockOSSClient))
	_, err := svc.CreateTask(context.Background(), 1, dto.CreateTaskRequest{Prompt: "test"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrInsufficientCredits)
}

func TestTaskService_CreateTask_LuaFail_Rollback(t *testing.T) {
	rdb := new(mockRedis)
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)

	rdb.On("Get", mock.Anything, "calliope:queue:depth").Return("0", nil)
	creditRepo.On("TryDeduct", mock.Anything, uint64(1), mock.AnythingOfType("string")).Return(nil)
	taskRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Task")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.Task).ID = 99
		})
	// Lua XADD+INCR fails
	rdb.On("Eval", mock.Anything, mock.AnythingOfType("string"), mock.Anything, mock.Anything).
		Return(nil, errors.New("redis error"))

	// Rollback: refund credit + mark task failed
	creditRepo.On("Refund", mock.Anything, uint64(1), mock.AnythingOfType("string")).Return(nil)
	taskRepo.On("UpdateStatus", mock.Anything, uint64(99), mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	_, err := svc.CreateTask(context.Background(), 1, dto.CreateTaskRequest{Prompt: "test"})
	require.Error(t, err)

	creditRepo.AssertCalled(t, "Refund", mock.Anything, uint64(1), mock.AnythingOfType("string"))
	taskRepo.AssertCalled(t, "UpdateStatus", mock.Anything, uint64(99), mock.Anything)
}

func TestTaskService_CreateTask_Success(t *testing.T) {
	rdb := new(mockRedis)
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)

	rdb.On("Get", mock.Anything, "calliope:queue:depth").Return("3", nil)
	creditRepo.On("TryDeduct", mock.Anything, uint64(1), mock.AnythingOfType("string")).Return(nil)
	taskRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Task")).
		Return(nil).
		Run(func(args mock.Arguments) {
			task := args.Get(1).(*model.Task)
			task.ID = 42
			task.QueuePosition = intPtr(3)
		})
	// Lua returns [streamID, newDepth]
	rdb.On("Eval", mock.Anything, mock.AnythingOfType("string"), mock.Anything, mock.Anything).
		Return([]interface{}{"1-0", int64(4)}, nil)
	// best-effort queue_position update
	taskRepo.On("UpdateStatus", mock.Anything, uint64(42), mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	resp, err := svc.CreateTask(context.Background(), 1, dto.CreateTaskRequest{Prompt: "test"})
	require.NoError(t, err)
	assert.Equal(t, uint64(42), resp.TaskID)
	assert.Equal(t, "queued", resp.Status)
	assert.Equal(t, 3, resp.QueuePosition)
}

// ---------------------------------------------------------------------------
// GetTask tests
// ---------------------------------------------------------------------------

func TestTaskService_GetTask_NotFound(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	taskRepo.On("FindByID", mock.Anything, uint64(99)).Return(nil, apierrors.ErrNotFound)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	_, err := svc.GetTask(context.Background(), 1, 99)
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrNotFound)
}

func TestTaskService_GetTask_Forbidden(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 999}, nil) // belongs to user 999

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	_, err := svc.GetTask(context.Background(), 1, 1) // request as user 1
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrForbidden)
}

func TestTaskService_GetTask_Queued(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	pos := 2
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "queued", Prompt: "test", Mode: "vocal", QueuePosition: &pos}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	resp, err := svc.GetTask(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "queued", resp.Status)
	require.NotNil(t, resp.QueuePosition)
	assert.Equal(t, 2, *resp.QueuePosition)
	assert.Nil(t, resp.Progress)
	assert.Nil(t, resp.Candidates)
}

func TestTaskService_GetTask_Processing(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	startedAt := time.Now().Add(-30 * time.Second)
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "processing", Prompt: "test", Mode: "vocal", StartedAt: &startedAt}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	resp, err := svc.GetTask(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "processing", resp.Status)
	require.NotNil(t, resp.Progress)
	assert.GreaterOrEqual(t, *resp.Progress, 0)
	assert.LessOrEqual(t, *resp.Progress, 90)
	assert.Nil(t, resp.QueuePosition)
}

func TestTaskService_GetTask_Completed(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	oss := new(mockOSSClient)

	keyA := "audio/1/1/candidate_a.mp3"
	keyB := "audio/1/1/candidate_b.mp3"
	dur := 30
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{
			ID: 1, UserID: 1, Status: "completed", Prompt: "test", Mode: "vocal",
			CandidateAKey: &keyA, CandidateBKey: &keyB, DurationSeconds: &dur,
		}, nil)
	oss.On("SignURL", mock.Anything, keyA, time.Hour).Return("https://cdn.example.com/a.mp3", nil)
	oss.On("SignURL", mock.Anything, keyB, time.Hour).Return("https://cdn.example.com/b.mp3", nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), oss)
	resp, err := svc.GetTask(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
	require.Len(t, resp.Candidates, 2)
	assert.Equal(t, "a", resp.Candidates[0].Index)
	assert.Equal(t, "https://cdn.example.com/a.mp3", resp.Candidates[0].URL)
	assert.Equal(t, "b", resp.Candidates[1].Index)
	oss.AssertExpectations(t)
}

func TestTaskService_GetTask_Failed(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	reason := "推理超时"
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "failed", Prompt: "test", Mode: "vocal", FailReason: &reason}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	resp, err := svc.GetTask(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "failed", resp.Status)
	require.NotNil(t, resp.FailReason)
	assert.Equal(t, "推理超时", *resp.FailReason)
}

// ---------------------------------------------------------------------------
// UpdateTaskStatus tests
// ---------------------------------------------------------------------------

func TestTaskService_UpdateTaskStatus_UnknownStatus(t *testing.T) {
	svc := newTestTaskService(new(mockTaskRepo), new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{Status: "unknown"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrInvalidTransition)
}

func TestTaskService_UpdateTaskStatus_Processing(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	rdb := new(mockRedis)

	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"queued"}).
		Return(true, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), rdb, new(mockOSSClient))
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{Status: "processing"})
	require.NoError(t, err)

	// DECR and PUBLISH must NOT be called for non-terminal status
	rdb.AssertNotCalled(t, "Decr", mock.Anything, mock.Anything)
	rdb.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything, mock.Anything)
}

func TestTaskService_UpdateTaskStatus_Completed(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)
	rdb := new(mockRedis)

	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"processing"}).
		Return(true, nil)
	rdb.On("Decr", mock.Anything, "calliope:queue:depth").Return(int64(3), nil)
	rdb.On("Publish", mock.Anything, "calliope:ws:task:1", mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	dur := 30
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{
		Status: "completed", DurationSeconds: &dur,
	})
	require.NoError(t, err)

	rdb.AssertCalled(t, "Decr", mock.Anything, "calliope:queue:depth")
	rdb.AssertCalled(t, "Publish", mock.Anything, "calliope:ws:task:1", mock.Anything)
	// No refund on completed
	creditRepo.AssertNotCalled(t, "Refund", mock.Anything, mock.Anything, mock.Anything)
}

func TestTaskService_UpdateTaskStatus_Failed_FromProcessing(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)
	rdb := new(mockRedis)

	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"queued", "processing"}).
		Return(true, nil)
	// Load task for UserID/CreditDate after successful failed update
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "failed", CreditDate: "2026-03-20"}, nil)
	creditRepo.On("Refund", mock.Anything, uint64(1), "2026-03-20").Return(nil)
	rdb.On("Decr", mock.Anything, "calliope:queue:depth").Return(int64(2), nil)
	rdb.On("Publish", mock.Anything, "calliope:ws:task:1", mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	reason := "推理错误"
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{
		Status: "failed", FailReason: &reason,
	})
	require.NoError(t, err)

	creditRepo.AssertCalled(t, "Refund", mock.Anything, uint64(1), "2026-03-20")
	rdb.AssertCalled(t, "Decr", mock.Anything, "calliope:queue:depth")
	rdb.AssertCalled(t, "Publish", mock.Anything, "calliope:ws:task:1", mock.Anything)
}

func TestTaskService_UpdateTaskStatus_Failed_FromQueued(t *testing.T) {
	// Protocol §3.4: worker must be able to fail a task that is still queued.
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)
	rdb := new(mockRedis)

	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"queued", "processing"}).
		Return(true, nil)
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 2, Status: "failed", CreditDate: "2026-03-20"}, nil)
	creditRepo.On("Refund", mock.Anything, uint64(2), "2026-03-20").Return(nil)
	rdb.On("Decr", mock.Anything, "calliope:queue:depth").Return(int64(1), nil)
	rdb.On("Publish", mock.Anything, "calliope:ws:task:1", mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	reason := "入队失败"
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{
		Status: "failed", FailReason: &reason,
	})
	require.NoError(t, err)
	creditRepo.AssertCalled(t, "Refund", mock.Anything, uint64(2), "2026-03-20")
}

func TestTaskService_UpdateTaskStatus_Duplicate(t *testing.T) {
	taskRepo := new(mockTaskRepo)

	// Conditional update matched 0 rows; task is already processing (duplicate callback)
	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"queued"}).
		Return(false, nil)
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "processing"}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{Status: "processing"})
	require.Error(t, err)

	var conflictErr *service.CallbackConflictError
	require.True(t, errors.As(err, &conflictErr))
	assert.Equal(t, "duplicate", conflictErr.Reason)
	assert.Equal(t, "processing", conflictErr.CurrentStatus)
}

func TestTaskService_UpdateTaskStatus_InvalidTransition(t *testing.T) {
	taskRepo := new(mockTaskRepo)

	// Conditional update matched 0 rows; task is completed (terminal, cannot transition)
	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(1), mock.Anything, []string{"queued", "processing"}).
		Return(false, nil)
	taskRepo.On("FindByID", mock.Anything, uint64(1)).
		Return(&model.Task{ID: 1, UserID: 1, Status: "completed"}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	err := svc.UpdateTaskStatus(context.Background(), 1, dto.UpdateTaskStatusRequest{Status: "failed"})
	require.Error(t, err)

	var conflictErr *service.CallbackConflictError
	require.True(t, errors.As(err, &conflictErr))
	assert.Equal(t, "invalid_transition", conflictErr.Reason)
	assert.Equal(t, "completed", conflictErr.CurrentStatus)
}

func TestTaskService_UpdateTaskStatus_NotFound(t *testing.T) {
	taskRepo := new(mockTaskRepo)

	taskRepo.On("ConditionalUpdateStatus", mock.Anything, uint64(99), mock.Anything, []string{"queued"}).
		Return(false, nil)
	taskRepo.On("FindByID", mock.Anything, uint64(99)).
		Return(nil, apierrors.ErrNotFound)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	err := svc.UpdateTaskStatus(context.Background(), 99, dto.UpdateTaskStatusRequest{Status: "processing"})
	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrNotFound)
}

// ---------------------------------------------------------------------------
// FixTimedOutTasks tests
// ---------------------------------------------------------------------------

func TestTaskService_FixTimedOutTasks_NoTimeout(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	taskRepo.On("FindTimedOutProcessing", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]*model.Task{}, nil)

	svc := newTestTaskService(taskRepo, new(mockCreditRepo), new(mockRedis), new(mockOSSClient))
	err := svc.FixTimedOutTasks(context.Background())
	require.NoError(t, err)
}

func TestTaskService_FixTimedOutTasks_WithTimeout(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)
	rdb := new(mockRedis)

	timedOut := []*model.Task{
		{ID: 10, UserID: 5, Status: "processing", CreditDate: "2026-03-20"},
		{ID: 11, UserID: 6, Status: "processing", CreditDate: "2026-03-19"},
	}
	taskRepo.On("FindTimedOutProcessing", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(timedOut, nil)
	taskRepo.On("ConditionalUpdateStatus", mock.Anything, mock.Anything, mock.Anything, []string{"processing"}).
		Return(true, nil)
	creditRepo.On("Refund", mock.Anything, uint64(5), "2026-03-20").Return(nil)
	creditRepo.On("Refund", mock.Anything, uint64(6), "2026-03-19").Return(nil)
	rdb.On("Decr", mock.Anything, "calliope:queue:depth").Return(int64(1), nil)
	rdb.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	err := svc.FixTimedOutTasks(context.Background())
	require.NoError(t, err)

	creditRepo.AssertNumberOfCalls(t, "Refund", 2)
	rdb.AssertNumberOfCalls(t, "Decr", 2)
	rdb.AssertNumberOfCalls(t, "Publish", 2)
}

func TestTaskService_FixTimedOutTasks_RaceWorkerCompletedFirst(t *testing.T) {
	// Simulate: worker completed the task between FindTimedOutProcessing and
	// ConditionalUpdateStatus. updated=false → no refund, no DECR, no publish.
	taskRepo := new(mockTaskRepo)
	creditRepo := new(mockCreditRepo)
	rdb := new(mockRedis)

	timedOut := []*model.Task{
		{ID: 10, UserID: 5, Status: "processing", CreditDate: "2026-03-20"},
	}
	taskRepo.On("FindTimedOutProcessing", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(timedOut, nil)
	taskRepo.On("ConditionalUpdateStatus", mock.Anything, mock.Anything, mock.Anything, []string{"processing"}).
		Return(false, nil) // worker won the race

	svc := newTestTaskService(taskRepo, creditRepo, rdb, new(mockOSSClient))
	err := svc.FixTimedOutTasks(context.Background())
	require.NoError(t, err)

	creditRepo.AssertNumberOfCalls(t, "Refund", 0)
	rdb.AssertNumberOfCalls(t, "Decr", 0)
	rdb.AssertNumberOfCalls(t, "Publish", 0)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func intPtr(i int) *int { return &i }
