package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/calliope/api/internal/model"
	apierrors "github.com/calliope/api/pkg/errors"
)

// TaskRepository defines the data-access contract for tasks.
type TaskRepository interface {
	// Create inserts a new task; DB fills in the ID.
	Create(ctx context.Context, task *model.Task) error

	// FindByID returns a task by primary key, or ErrNotFound.
	FindByID(ctx context.Context, id uint64) (*model.Task, error)

	// UpdateStatus updates the given fields on a task.
	UpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}) error

	// ConditionalUpdateStatus updates fields on a task only when its current status
	// is in fromStatuses. Returns (true, nil) if at least one row was updated,
	// (false, nil) if no rows matched (precondition not met), or (false, err) on
	// database error.
	ConditionalUpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}, fromStatuses []string) (bool, error)

	// FindTimedOutProcessing returns tasks with status=processing and started_at < cutoff.
	FindTimedOutProcessing(ctx context.Context, cutoff time.Time) ([]*model.Task, error)
}

type taskRepositoryImpl struct {
	db *gorm.DB
}

// NewTaskRepository returns a GORM-backed TaskRepository.
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepositoryImpl{db: db}
}

func (r *taskRepositoryImpl) Create(ctx context.Context, task *model.Task) error {
	if err := r.db.WithContext(ctx).Create(task).Error; err != nil {
		return fmt.Errorf("taskRepository.Create: %w", err)
	}
	return nil
}

func (r *taskRepositoryImpl) FindByID(ctx context.Context, id uint64) (*model.Task, error) {
	var task model.Task
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("taskRepository.FindByID: %w", err)
	}
	return &task, nil
}

func (r *taskRepositoryImpl) UpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}) error {
	result := r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return fmt.Errorf("taskRepository.UpdateStatus: %w", result.Error)
	}
	return nil
}

func (r *taskRepositoryImpl) ConditionalUpdateStatus(ctx context.Context, id uint64, fields map[string]interface{}, fromStatuses []string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status IN ?", id, fromStatuses).
		Updates(fields)
	if result.Error != nil {
		return false, fmt.Errorf("taskRepository.ConditionalUpdateStatus: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *taskRepositoryImpl) FindTimedOutProcessing(ctx context.Context, cutoff time.Time) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("status = ? AND started_at < ?", "processing", cutoff).
		Limit(100).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("taskRepository.FindTimedOutProcessing: %w", err)
	}
	return tasks, nil
}
