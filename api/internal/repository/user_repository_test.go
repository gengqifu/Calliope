//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/repository"
	apierrors "github.com/calliope/api/pkg/errors"
)

var testDB *gorm.DB

const (
	migrationsPath = "../../migrations"
	defaultTestDSN = "calliope:calliope@tcp(localhost:3306)/calliope_test?charset=utf8mb4&parseTime=True&loc=Local"
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	var err error
	testDB, err = gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Printf("failed to open test DB: %v\n", err)
		os.Exit(1)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		fmt.Printf("failed to get sql.DB: %v\n", err)
		os.Exit(1)
	}

	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		fmt.Printf("failed to create migrate driver: %v\n", err)
		os.Exit(1)
	}

	mg, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "mysql", driver)
	if err != nil {
		fmt.Printf("failed to create migrator: %v\n", err)
		os.Exit(1)
	}

	if err := mg.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Printf("failed to run migrations: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Teardown: rollback all migrations
	if err := mg.Down(); err != nil && err != migrate.ErrNoChange {
		fmt.Printf("failed to rollback migrations: %v\n", err)
	}

	os.Exit(code)
}

func cleanUsers(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec("DELETE FROM users").Error)
}

func TestUserRepository_Create(t *testing.T) {
	repo := repository.NewUserRepository(testDB)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		cleanUsers(t)
		user := &model.User{
			Email:    "alice@example.com",
			Password: "$2a$12$hashedpassword00000000000000000000000000000000000000000",
			Nickname: "Alice",
		}
		err := repo.Create(ctx, user)
		require.NoError(t, err)
		assert.NotZero(t, user.ID)
	})

	t.Run("duplicate email returns error", func(t *testing.T) {
		cleanUsers(t)
		user := &model.User{Email: "bob@example.com", Password: "hash"}
		require.NoError(t, repo.Create(ctx, user))

		err := repo.Create(ctx, &model.User{Email: "bob@example.com", Password: "hash2"})
		require.Error(t, err)
	})
}

func TestUserRepository_FindByEmail(t *testing.T) {
	repo := repository.NewUserRepository(testDB)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		cleanUsers(t)
		original := &model.User{Email: "carol@example.com", Password: "hash", Nickname: "Carol"}
		require.NoError(t, repo.Create(ctx, original))

		found, err := repo.FindByEmail(ctx, "carol@example.com")
		require.NoError(t, err)
		assert.Equal(t, original.ID, found.ID)
		assert.Equal(t, "Carol", found.Nickname)
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		cleanUsers(t)
		_, err := repo.FindByEmail(ctx, "nobody@example.com")
		require.Error(t, err)
		assert.ErrorIs(t, err, apierrors.ErrNotFound)
	})
}

func TestUserRepository_FindByID(t *testing.T) {
	repo := repository.NewUserRepository(testDB)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		cleanUsers(t)
		user := &model.User{Email: "dave@example.com", Password: "hash"}
		require.NoError(t, repo.Create(ctx, user))

		found, err := repo.FindByID(ctx, user.ID)
		require.NoError(t, err)
		assert.Equal(t, user.ID, found.ID)
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		cleanUsers(t)
		_, err := repo.FindByID(ctx, 999999)
		require.Error(t, err)
		assert.ErrorIs(t, err, apierrors.ErrNotFound)
	})
}
