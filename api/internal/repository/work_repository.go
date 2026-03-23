package repository

import (
	"context"
	"errors"
	"fmt"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/calliope/api/internal/model"
	apierrors "github.com/calliope/api/pkg/errors"
)

// WorkRepository defines data access operations for works.
type WorkRepository interface {
	// Create inserts a new work record and populates work.ID on success.
	Create(ctx context.Context, work *model.Work) error

	// UpdateAudioKey updates the audio_key field for the given work ID.
	UpdateAudioKey(ctx context.Context, id uint64, audioKey string) error

	// FindByID returns the work with the given ID, or ErrNotFound if absent.
	// Soft-deleted works are excluded.
	FindByID(ctx context.Context, id uint64) (*model.Work, error)

	// FindByUserIDAndID returns the work only when it belongs to userID.
	// Returns ErrNotFound if absent, ErrForbidden if owned by another user.
	FindByUserIDAndID(ctx context.Context, userID, id uint64) (*model.Work, error)

	// ListByUserID returns a paginated list of works for the user (newest first).
	// Returns items, total count, and error.
	ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.Work, int64, error)

	// SoftDelete sets deleted_at on the given work.
	SoftDelete(ctx context.Context, id uint64) error

	// Delete permanently removes the work record (used for rollback only).
	Delete(ctx context.Context, id uint64) error

	// ExistsByTaskID reports whether a non-deleted work already exists for taskID.
	ExistsByTaskID(ctx context.Context, taskID uint64) (bool, error)
}

type workRepositoryImpl struct {
	db *gorm.DB
}

// NewWorkRepository creates a new WorkRepository backed by GORM.
func NewWorkRepository(db *gorm.DB) WorkRepository {
	return &workRepositoryImpl{db: db}
}

func (r *workRepositoryImpl) Create(ctx context.Context, work *model.Work) error {
	if err := r.db.WithContext(ctx).Create(work).Error; err != nil {
		var mysqlErr *gomysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return apierrors.ErrWorkAlreadySaved
		}
		return fmt.Errorf("workRepository.Create: %w", err)
	}
	return nil
}

func (r *workRepositoryImpl) UpdateAudioKey(ctx context.Context, id uint64, audioKey string) error {
	result := r.db.WithContext(ctx).
		Model(&model.Work{}).
		Where("id = ?", id).
		Update("audio_key", audioKey)
	if result.Error != nil {
		return fmt.Errorf("workRepository.UpdateAudioKey: %w", result.Error)
	}
	return nil
}

func (r *workRepositoryImpl) FindByID(ctx context.Context, id uint64) (*model.Work, error) {
	var work model.Work
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&work).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("workRepository.FindByID: %w", err)
	}
	return &work, nil
}

func (r *workRepositoryImpl) FindByUserIDAndID(ctx context.Context, userID, id uint64) (*model.Work, error) {
	// First fetch without ownership filter to distinguish not-found vs forbidden.
	work, err := r.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if work.UserID != userID {
		return nil, apierrors.ErrForbidden
	}
	return work, nil
}

func (r *workRepositoryImpl) ListByUserID(ctx context.Context, userID uint64, offset, limit int) ([]*model.Work, int64, error) {
	var works []*model.Work
	var total int64

	base := r.db.WithContext(ctx).Model(&model.Work{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("workRepository.ListByUserID count: %w", err)
	}

	if err := base.Order("created_at DESC").Offset(offset).Limit(limit).Find(&works).Error; err != nil {
		return nil, 0, fmt.Errorf("workRepository.ListByUserID find: %w", err)
	}

	return works, total, nil
}

func (r *workRepositoryImpl) SoftDelete(ctx context.Context, id uint64) error {
	result := r.db.WithContext(ctx).
		Model(&model.Work{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", gorm.Expr("NOW()"))
	if result.Error != nil {
		return fmt.Errorf("workRepository.SoftDelete: %w", result.Error)
	}
	return nil
}

func (r *workRepositoryImpl) Delete(ctx context.Context, id uint64) error {
	if err := r.db.WithContext(ctx).Unscoped().Delete(&model.Work{}, id).Error; err != nil {
		return fmt.Errorf("workRepository.Delete: %w", err)
	}
	return nil
}

// ExistsByTaskID reports whether a work (active or soft-deleted) already exists
// for taskID. uk_task_id is an unconditional unique key, so a soft-deleted work
// still occupies the slot — re-saving the same task is not allowed.
func (r *workRepositoryImpl) ExistsByTaskID(ctx context.Context, taskID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Work{}).
		Where("task_id = ?", taskID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("workRepository.ExistsByTaskID: %w", err)
	}
	return count > 0, nil
}
