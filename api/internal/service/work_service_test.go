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
// Mock: WorkRepository
// ---------------------------------------------------------------------------

type mockWorkRepo struct{ mock.Mock }

func (m *mockWorkRepo) Create(ctx context.Context, work *model.Work) error {
	args := m.Called(ctx, work)
	return args.Error(0)
}

func (m *mockWorkRepo) UpdateAudioKey(ctx context.Context, id uint64, audioKey string) error {
	return m.Called(ctx, id, audioKey).Error(0)
}

func (m *mockWorkRepo) FindByID(ctx context.Context, id uint64) (*model.Work, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Work), args.Error(1)
}

func (m *mockWorkRepo) FindByUserIDAndID(ctx context.Context, userID, id uint64) (*model.Work, error) {
	args := m.Called(ctx, userID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Work), args.Error(1)
}

func (m *mockWorkRepo) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.Work, int64, error) {
	args := m.Called(ctx, userID, offset, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*model.Work), args.Get(1).(int64), args.Error(2)
}

func (m *mockWorkRepo) SoftDelete(ctx context.Context, id uint64) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockWorkRepo) Delete(ctx context.Context, id uint64) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockWorkRepo) ExistsByTaskID(ctx context.Context, taskID uint64) (bool, error) {
	args := m.Called(ctx, taskID)
	return args.Bool(0), args.Error(1)
}

// ---------------------------------------------------------------------------
// Mock: OSSWorksClient
// ---------------------------------------------------------------------------

type mockOSSWorksClient struct{ mock.Mock }

func (m *mockOSSWorksClient) SignURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	args := m.Called(ctx, key, ttl)
	return args.String(0), args.Error(1)
}

func (m *mockOSSWorksClient) Copy(ctx context.Context, srcKey, dstKey string) error {
	return m.Called(ctx, srcKey, dstKey).Error(0)
}

func (m *mockOSSWorksClient) Delete(ctx context.Context, key string) error {
	return m.Called(ctx, key).Error(0)
}

// ---------------------------------------------------------------------------
// Helper: newTestWorkService
// ---------------------------------------------------------------------------

func newTestWorkService(workRepo *mockWorkRepo, taskRepo *mockTaskRepo, oss *mockOSSWorksClient) service.WorkService {
	cfg := service.WorkServiceConfig{SignedURLTTL: time.Hour}
	return service.NewWorkService(cfg, workRepo, taskRepo, oss)
}

func completedTask(userID, taskID uint64) *model.Task {
	now := time.Now()
	dur := 30
	keyA := "audio/1/123/candidate_a.mp3"
	keyB := "audio/1/123/candidate_b.mp3"
	return &model.Task{
		ID:              taskID,
		UserID:          userID,
		Prompt:          "test prompt",
		Mode:            "vocal",
		Status:          "completed",
		CandidateAKey:   &keyA,
		CandidateBKey:   &keyB,
		DurationSeconds: &dur,
		CompletedAt:     &now,
	}
}

// ---------------------------------------------------------------------------
// SaveWork tests
// ---------------------------------------------------------------------------

func TestWorkService_SaveWork_Success_CandidateA(t *testing.T) {
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)
	oss := new(mockOSSWorksClient)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(false, nil)
	workRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Work")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.Work).ID = 88
		})
	oss.On("Copy", mock.Anything, *task.CandidateAKey, "works/1/88.mp3").Return(nil)
	workRepo.On("UpdateAudioKey", mock.Anything, uint64(88), "works/1/88.mp3").Return(nil)
	oss.On("SignURL", mock.Anything, "works/1/88.mp3", time.Hour).Return("https://cdn/works/1/88.mp3?token=xxx", nil)

	svc := newTestWorkService(workRepo, taskRepo, oss)
	resp, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{
		TaskID:    123,
		Candidate: "a",
		Title:     "My Song",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(88), resp.ID)
	assert.Equal(t, "My Song", resp.Title)
	assert.Equal(t, "https://cdn/works/1/88.mp3?token=xxx", resp.AudioURL)
	oss.AssertExpectations(t)
	workRepo.AssertExpectations(t)
}

func TestWorkService_SaveWork_Success_CandidateB(t *testing.T) {
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)
	oss := new(mockOSSWorksClient)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(false, nil)
	workRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Work")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.Work).ID = 89
		})
	oss.On("Copy", mock.Anything, *task.CandidateBKey, "works/1/89.mp3").Return(nil)
	workRepo.On("UpdateAudioKey", mock.Anything, uint64(89), "works/1/89.mp3").Return(nil)
	oss.On("SignURL", mock.Anything, "works/1/89.mp3", time.Hour).Return("https://cdn/works/1/89.mp3?token=xxx", nil)

	svc := newTestWorkService(workRepo, taskRepo, oss)
	resp, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{
		TaskID:    123,
		Candidate: "b",
		Title:     "Song B",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(89), resp.ID)
	oss.AssertExpectations(t)
}

func TestWorkService_SaveWork_TaskNotFound(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	taskRepo.On("FindByID", mock.Anything, uint64(999)).Return(nil, apierrors.ErrNotFound)

	svc := newTestWorkService(new(mockWorkRepo), taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 999, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrNotFound)
}

func TestWorkService_SaveWork_Forbidden(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	task := completedTask(2, 123) // owned by user 2
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)

	svc := newTestWorkService(new(mockWorkRepo), taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1 /*different user*/, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrForbidden)
}

func TestWorkService_SaveWork_TaskNotCompleted(t *testing.T) {
	taskRepo := new(mockTaskRepo)
	task := completedTask(1, 123)
	task.Status = "queued" // not completed
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)

	svc := newTestWorkService(new(mockWorkRepo), taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrTaskNotCompleted)
}

func TestWorkService_SaveWork_AlreadySaved(t *testing.T) {
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(true, nil)

	svc := newTestWorkService(workRepo, taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrWorkAlreadySaved)
}

func TestWorkService_SaveWork_OSSCopyFails_RollsBack(t *testing.T) {
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)
	oss := new(mockOSSWorksClient)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(false, nil)
	workRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Work")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.Work).ID = 88
		})
	oss.On("Copy", mock.Anything, *task.CandidateAKey, "works/1/88.mp3").
		Return(errors.New("qiniu: network error"))
	workRepo.On("Delete", mock.Anything, uint64(88)).Return(nil) // rollback

	svc := newTestWorkService(workRepo, taskRepo, oss)
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	workRepo.AssertCalled(t, "Delete", mock.Anything, uint64(88))
}

func TestWorkService_SaveWork_UpdateAudioKeyFails_FullRollback(t *testing.T) {
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)
	oss := new(mockOSSWorksClient)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(false, nil)
	workRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Work")).
		Return(nil).
		Run(func(args mock.Arguments) {
			args.Get(1).(*model.Work).ID = 88
		})
	oss.On("Copy", mock.Anything, *task.CandidateAKey, "works/1/88.mp3").Return(nil)
	workRepo.On("UpdateAudioKey", mock.Anything, uint64(88), "works/1/88.mp3").
		Return(errors.New("db error"))
	// Full rollback: both the work record and the OSS copy must be cleaned up.
	workRepo.On("Delete", mock.Anything, uint64(88)).Return(nil)
	oss.On("Delete", mock.Anything, "works/1/88.mp3").Return(nil)

	svc := newTestWorkService(workRepo, taskRepo, oss)
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	workRepo.AssertCalled(t, "Delete", mock.Anything, uint64(88))
	oss.AssertCalled(t, "Delete", mock.Anything, "works/1/88.mp3")
}

func TestWorkService_SaveWork_AfterSoftDelete_ReturnsWorkAlreadySaved(t *testing.T) {
	// uk_task_id is unconditional: a soft-deleted work still occupies the slot.
	// ExistsByTaskID must include soft-deleted rows so we return WORK_ALREADY_SAVED,
	// not fall through to Create and hit a 1062 DB error.
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	// Simulate: a soft-deleted work exists for this task_id.
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(true, nil)

	svc := newTestWorkService(workRepo, taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrWorkAlreadySaved)
	workRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestWorkService_SaveWork_ConcurrentDuplicate_ReturnsWorkAlreadySaved(t *testing.T) {
	// Simulates two concurrent SaveWork calls passing ExistsByTaskID check,
	// where the second insert hits the uk_task_id unique constraint.
	// The repository converts MySQL 1062 → ErrWorkAlreadySaved, so the service
	// must propagate it (not wrap it as a 500).
	workRepo := new(mockWorkRepo)
	taskRepo := new(mockTaskRepo)

	task := completedTask(1, 123)
	taskRepo.On("FindByID", mock.Anything, uint64(123)).Return(task, nil)
	workRepo.On("ExistsByTaskID", mock.Anything, uint64(123)).Return(false, nil) // race: both pass
	workRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Work")).
		Return(apierrors.ErrWorkAlreadySaved) // repository converted 1062

	svc := newTestWorkService(workRepo, taskRepo, new(mockOSSWorksClient))
	_, err := svc.SaveWork(context.Background(), 1, dto.SaveWorkRequest{TaskID: 123, Candidate: "a", Title: "x"})

	require.Error(t, err)
	assert.ErrorIs(t, err, apierrors.ErrWorkAlreadySaved)
}

// ---------------------------------------------------------------------------
// GetWork tests
// ---------------------------------------------------------------------------

func TestWorkService_GetWork_Success(t *testing.T) {
	workRepo := new(mockWorkRepo)
	oss := new(mockOSSWorksClient)

	w := &model.Work{
		ID:              88,
		UserID:          1,
		Title:           "My Song",
		Prompt:          "test",
		Mode:            "vocal",
		AudioKey:        "works/1/88.mp3",
		DurationSeconds: 30,
		CreatedAt:       time.Now(),
	}
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(1), uint64(88)).Return(w, nil)
	oss.On("SignURL", mock.Anything, "works/1/88.mp3", time.Hour).Return("https://cdn/x", nil)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), oss)
	resp, err := svc.GetWork(context.Background(), 1, 88)

	require.NoError(t, err)
	assert.Equal(t, uint64(88), resp.ID)
	assert.Equal(t, "https://cdn/x", resp.AudioURL)
}

func TestWorkService_GetWork_NotFound(t *testing.T) {
	workRepo := new(mockWorkRepo)
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(1), uint64(999)).Return(nil, apierrors.ErrNotFound)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	_, err := svc.GetWork(context.Background(), 1, 999)

	assert.ErrorIs(t, err, apierrors.ErrNotFound)
}

func TestWorkService_GetWork_Forbidden(t *testing.T) {
	workRepo := new(mockWorkRepo)
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(2), uint64(88)).Return(nil, apierrors.ErrForbidden)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	_, err := svc.GetWork(context.Background(), 2, 88)

	assert.ErrorIs(t, err, apierrors.ErrForbidden)
}

// ---------------------------------------------------------------------------
// ListWorks tests
// ---------------------------------------------------------------------------

func TestWorkService_ListWorks_Success(t *testing.T) {
	workRepo := new(mockWorkRepo)
	oss := new(mockOSSWorksClient)

	works := []*model.Work{
		{ID: 1, UserID: 1, AudioKey: "works/1/1.mp3", CreatedAt: time.Now()},
		{ID: 2, UserID: 1, AudioKey: "works/1/2.mp3", CreatedAt: time.Now()},
	}
	workRepo.On("ListByUserID", mock.Anything, uint64(1), 0, 20).Return(works, int64(2), nil)
	oss.On("SignURL", mock.Anything, mock.AnythingOfType("string"), time.Hour).Return("https://cdn/x", nil)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), oss)
	resp, err := svc.ListWorks(context.Background(), 1, dto.ListWorksRequest{Page: 1, PageSize: 20})

	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Items, 2)
}

func TestWorkService_ListWorks_DefaultPagination(t *testing.T) {
	workRepo := new(mockWorkRepo)
	oss := new(mockOSSWorksClient)

	workRepo.On("ListByUserID", mock.Anything, uint64(1), 0, 20).Return([]*model.Work{}, int64(0), nil)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), oss)
	resp, err := svc.ListWorks(context.Background(), 1, dto.ListWorksRequest{})

	require.NoError(t, err)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 20, resp.PageSize)
}

// ---------------------------------------------------------------------------
// GetDownloadURL tests
// ---------------------------------------------------------------------------

func TestWorkService_GetDownloadURL_Success(t *testing.T) {
	workRepo := new(mockWorkRepo)
	oss := new(mockOSSWorksClient)

	w := &model.Work{
		ID:       88,
		UserID:   1,
		Title:    "My Song",
		AudioKey: "works/1/88.mp3",
	}
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(1), uint64(88)).Return(w, nil)
	oss.On("SignURL", mock.Anything, "works/1/88.mp3", time.Hour).
		Return("https://cdn/works/1/88.mp3?e=123&token=xxx", nil)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), oss)
	resp, err := svc.GetDownloadURL(context.Background(), 1, 88)

	require.NoError(t, err)
	assert.Equal(t, "My Song.mp3", resp.Filename)
	assert.Contains(t, resp.DownloadURL, "attname=")
	assert.Equal(t, 3600, resp.ExpiresIn)
}

func TestWorkService_GetDownloadURL_Forbidden(t *testing.T) {
	workRepo := new(mockWorkRepo)
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(2), uint64(88)).Return(nil, apierrors.ErrForbidden)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	_, err := svc.GetDownloadURL(context.Background(), 2, 88)

	assert.ErrorIs(t, err, apierrors.ErrForbidden)
}

// ---------------------------------------------------------------------------
// DeleteWork tests
// ---------------------------------------------------------------------------

func TestWorkService_DeleteWork_Success(t *testing.T) {
	workRepo := new(mockWorkRepo)

	w := &model.Work{ID: 88, UserID: 1, AudioKey: "works/1/88.mp3"}
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(1), uint64(88)).Return(w, nil)
	workRepo.On("SoftDelete", mock.Anything, uint64(88)).Return(nil)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	err := svc.DeleteWork(context.Background(), 1, 88)

	require.NoError(t, err)
	workRepo.AssertCalled(t, "SoftDelete", mock.Anything, uint64(88))
}

func TestWorkService_DeleteWork_Forbidden(t *testing.T) {
	workRepo := new(mockWorkRepo)
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(2), uint64(88)).Return(nil, apierrors.ErrForbidden)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	err := svc.DeleteWork(context.Background(), 2, 88)

	assert.ErrorIs(t, err, apierrors.ErrForbidden)
}

func TestWorkService_DeleteWork_NotFound(t *testing.T) {
	workRepo := new(mockWorkRepo)
	workRepo.On("FindByUserIDAndID", mock.Anything, uint64(1), uint64(999)).Return(nil, apierrors.ErrNotFound)

	svc := newTestWorkService(workRepo, new(mockTaskRepo), new(mockOSSWorksClient))
	err := svc.DeleteWork(context.Background(), 1, 999)

	assert.ErrorIs(t, err, apierrors.ErrNotFound)
}
