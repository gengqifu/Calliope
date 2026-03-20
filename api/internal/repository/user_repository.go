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

// UserRepository defines the data-access contract for users.
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByID(ctx context.Context, id uint64) (*model.User, error)
}

type userRepositoryImpl struct {
	db *gorm.DB
}

// NewUserRepository returns a GORM-backed UserRepository.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepositoryImpl{db: db}
}

func (r *userRepositoryImpl) Create(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		var mysqlErr *gomysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return apierrors.ErrEmailAlreadyExists
		}
		return fmt.Errorf("userRepository.Create: %w", err)
	}
	return nil
}

func (r *userRepositoryImpl) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("userRepository.FindByEmail: %w", err)
	}
	return &user, nil
}

func (r *userRepositoryImpl) FindByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("userRepository.FindByID: %w", err)
	}
	return &user, nil
}
