package service

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/repository"
	apierrors "github.com/calliope/api/pkg/errors"
)

// OSSWorksClient is the subset of OSS operations required by WorkService.
type OSSWorksClient interface {
	SignURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	Copy(ctx context.Context, srcKey, dstKey string) error
	Delete(ctx context.Context, key string) error
}

// WorkServiceConfig holds all configuration needed by WorkService.
type WorkServiceConfig struct {
	SignedURLTTL time.Duration
}

// WorkService defines the work management business logic.
type WorkService interface {
	SaveWork(ctx context.Context, userID uint64, req dto.SaveWorkRequest) (*dto.WorkResponse, error)
	GetWork(ctx context.Context, userID, workID uint64) (*dto.WorkResponse, error)
	ListWorks(ctx context.Context, userID uint64, req dto.ListWorksRequest) (*dto.ListWorksResponse, error)
	GetDownloadURL(ctx context.Context, userID, workID uint64) (*dto.DownloadURLResponse, error)
	DeleteWork(ctx context.Context, userID, workID uint64) error
}

type workServiceImpl struct {
	cfg      WorkServiceConfig
	workRepo repository.WorkRepository
	taskRepo repository.TaskRepository
	oss      OSSWorksClient
}

// NewWorkService creates a new WorkService.
func NewWorkService(
	cfg WorkServiceConfig,
	workRepo repository.WorkRepository,
	taskRepo repository.TaskRepository,
	oss OSSWorksClient,
) WorkService {
	return &workServiceImpl{
		cfg:      cfg,
		workRepo: workRepo,
		taskRepo: taskRepo,
		oss:      oss,
	}
}

// SaveWork validates the task, copies the selected candidate to a permanent path,
// and inserts a work record.
func (s *workServiceImpl) SaveWork(ctx context.Context, userID uint64, req dto.SaveWorkRequest) (*dto.WorkResponse, error) {
	// 1. Validate task exists and belongs to the user.
	task, err := s.taskRepo.FindByID(ctx, req.TaskID)
	if err != nil {
		return nil, err // ErrNotFound propagates as-is
	}
	if task.UserID != userID {
		return nil, apierrors.ErrForbidden
	}
	if task.Status != "completed" {
		return nil, apierrors.ErrTaskNotCompleted
	}

	// 2. Ensure the task hasn't already been saved.
	exists, err := s.workRepo.ExistsByTaskID(ctx, req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("workService.SaveWork: check existing: %w", err)
	}
	if exists {
		return nil, apierrors.ErrWorkAlreadySaved
	}

	// 3. Resolve the candidate key.
	candidateKey, err := candidateKeyFromTask(task, req.Candidate)
	if err != nil {
		return nil, err
	}

	durationSeconds := 0
	if task.DurationSeconds != nil {
		durationSeconds = *task.DurationSeconds
	}

	// 4. Insert work record with placeholder audio_key (candidateKey).
	//    GORM populates work.ID after successful insert.
	work := &model.Work{
		UserID:          userID,
		TaskID:          task.ID,
		Title:           req.Title,
		Prompt:          task.Prompt,
		Mode:            task.Mode,
		AudioKey:        candidateKey, // placeholder; updated in step 6
		DurationSeconds: durationSeconds,
	}
	if err := s.workRepo.Create(ctx, work); err != nil {
		return nil, fmt.Errorf("workService.SaveWork: create work: %w", err)
	}

	// 5. Compute the permanent storage key now that we have work.ID.
	finalKey := fmt.Sprintf("works/%d/%d.mp3", userID, work.ID)

	// 6. Copy candidate → permanent key in OSS.
	if err := s.oss.Copy(ctx, candidateKey, finalKey); err != nil {
		// Best-effort rollback: remove the placeholder work record.
		_ = s.workRepo.Delete(ctx, work.ID)
		return nil, fmt.Errorf("workService.SaveWork: copy audio: %w", err)
	}

	// 7. Update the work record with the final audio key.
	if err := s.workRepo.UpdateAudioKey(ctx, work.ID, finalKey); err != nil {
		// Full rollback: remove the work record so the user can retry, and
		// best-effort delete the OSS copy we just created.
		_ = s.workRepo.Delete(ctx, work.ID)
		_ = s.oss.Delete(ctx, finalKey)
		return nil, fmt.Errorf("workService.SaveWork: update audio_key: %w", err)
	}
	work.AudioKey = finalKey

	// 8. Build and return the response with a signed URL.
	return s.buildWorkResponse(ctx, work)
}

// GetWork returns a single work belonging to userID with a fresh signed URL.
func (s *workServiceImpl) GetWork(ctx context.Context, userID, workID uint64) (*dto.WorkResponse, error) {
	work, err := s.workRepo.FindByUserIDAndID(ctx, userID, workID)
	if err != nil {
		return nil, err
	}
	return s.buildWorkResponse(ctx, work)
}

// ListWorks returns a paginated list of works for userID, newest first.
func (s *workServiceImpl) ListWorks(ctx context.Context, userID uint64, req dto.ListWorksRequest) (*dto.ListWorksResponse, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	works, total, err := s.workRepo.ListByUserID(ctx, userID, offset, pageSize)
	if err != nil {
		return nil, fmt.Errorf("workService.ListWorks: %w", err)
	}

	items := make([]dto.WorkResponse, 0, len(works))
	for _, w := range works {
		resp, err := s.buildWorkResponse(ctx, w)
		if err != nil {
			return nil, err
		}
		items = append(items, *resp)
	}

	return &dto.ListWorksResponse{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items,
	}, nil
}

// GetDownloadURL returns a signed download URL that triggers browser download.
func (s *workServiceImpl) GetDownloadURL(ctx context.Context, userID, workID uint64) (*dto.DownloadURLResponse, error) {
	work, err := s.workRepo.FindByUserIDAndID(ctx, userID, workID)
	if err != nil {
		return nil, err
	}

	ttl := s.cfg.SignedURLTTL
	if ttl == 0 {
		ttl = time.Hour
	}

	baseURL, err := s.oss.SignURL(ctx, work.AudioKey, ttl)
	if err != nil {
		return nil, fmt.Errorf("workService.GetDownloadURL: sign url: %w", err)
	}

	filename := work.Title + ".mp3"
	// Append attname query parameter to trigger browser download.
	downloadURL := baseURL + "&attname=" + url.QueryEscape(filename)

	return &dto.DownloadURLResponse{
		DownloadURL: downloadURL,
		Filename:    filename,
		ExpiresIn:   int(ttl.Seconds()),
	}, nil
}

// DeleteWork soft-deletes the work identified by workID if it belongs to userID.
func (s *workServiceImpl) DeleteWork(ctx context.Context, userID, workID uint64) error {
	work, err := s.workRepo.FindByUserIDAndID(ctx, userID, workID)
	if err != nil {
		return err
	}
	if err := s.workRepo.SoftDelete(ctx, work.ID); err != nil {
		return fmt.Errorf("workService.DeleteWork: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *workServiceImpl) buildWorkResponse(ctx context.Context, work *model.Work) (*dto.WorkResponse, error) {
	ttl := s.cfg.SignedURLTTL
	if ttl == 0 {
		ttl = time.Hour
	}

	audioURL, err := s.oss.SignURL(ctx, work.AudioKey, ttl)
	if err != nil {
		return nil, fmt.Errorf("workService: sign url: %w", err)
	}

	return &dto.WorkResponse{
		ID:                work.ID,
		Title:             work.Title,
		Prompt:            work.Prompt,
		Mode:              work.Mode,
		AudioURL:          audioURL,
		AudioURLExpiresAt: time.Now().Add(ttl),
		DurationSeconds:   work.DurationSeconds,
		PlayCount:         work.PlayCount,
		CreatedAt:         work.CreatedAt,
	}, nil
}

// candidateKeyFromTask resolves the OSS key for the selected candidate ("a" or "b").
func candidateKeyFromTask(task *model.Task, candidate string) (string, error) {
	switch candidate {
	case "a":
		if task.CandidateAKey == nil || *task.CandidateAKey == "" {
			return "", apierrors.ErrNotFound
		}
		return *task.CandidateAKey, nil
	case "b":
		if task.CandidateBKey == nil || *task.CandidateBKey == "" {
			return "", apierrors.ErrNotFound
		}
		return *task.CandidateBKey, nil
	default:
		return "", fmt.Errorf("workService: unknown candidate %q", candidate)
	}
}
