package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	apierrors "github.com/calliope/api/pkg/errors"
)

// CreditRepository defines the data-access contract for daily credits.
type CreditRepository interface {
	// TryDeduct atomically deducts one credit for the given user on the given date.
	// Returns ErrInsufficientCredits if the daily limit has been reached.
	TryDeduct(ctx context.Context, userID uint64, date string) error

	// Refund returns one credit to the given user for the given date (idempotent).
	Refund(ctx context.Context, userID uint64, date string) error
}

type creditRepositoryImpl struct {
	db *gorm.DB
}

// NewCreditRepository returns a GORM-backed CreditRepository.
func NewCreditRepository(db *gorm.DB) CreditRepository {
	return &creditRepositoryImpl{db: db}
}

// TryDeduct uses an INSERT … ON DUPLICATE KEY UPDATE statement to atomically
// increment the used count, then checks ROW_COUNT() to determine success.
//
// ROW_COUNT() semantics (MySQL):
//   - 1 → new row inserted (first use today)
//   - 2 → existing row updated and used actually changed (used < limit_count)
//   - 0 → no change (used already >= limit_count)
func (r *creditRepositoryImpl) TryDeduct(ctx context.Context, userID uint64, date string) error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return fmt.Errorf("creditRepository.TryDeduct: get sqlDB: %w", err)
	}

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("creditRepository.TryDeduct: acquire conn: %w", err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx,
		`INSERT INTO credits (user_id, date, used, limit_count)
		 VALUES (?, ?, 1, 5)
		 ON DUPLICATE KEY UPDATE
		   used = IF(used < limit_count, used + 1, used)`,
		userID, date,
	)
	if err != nil {
		return fmt.Errorf("creditRepository.TryDeduct: exec: %w", err)
	}

	var rowCount int64
	if err := conn.QueryRowContext(ctx, "SELECT ROW_COUNT()").Scan(&rowCount); err != nil {
		return fmt.Errorf("creditRepository.TryDeduct: row_count: %w", err)
	}

	// 0 means the IF condition was false → limit reached
	if rowCount == 0 {
		return apierrors.ErrInsufficientCredits
	}
	return nil
}

// Refund decrements used by 1, floored at 0 (idempotent).
func (r *creditRepositoryImpl) Refund(ctx context.Context, userID uint64, date string) error {
	err := r.db.WithContext(ctx).Exec(
		`UPDATE credits SET used = GREATEST(used - 1, 0) WHERE user_id = ? AND date = ?`,
		userID, date,
	).Error
	if err != nil {
		return fmt.Errorf("creditRepository.Refund: %w", err)
	}
	return nil
}
