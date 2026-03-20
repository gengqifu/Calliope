//go:build integration

package db

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB     *gorm.DB
	testDBName string
)

const (
	migrationsPath = "../../migrations"
	defaultTestDSN = "calliope:calliope@tcp(localhost:3306)/calliope?charset=utf8mb4&parseTime=True&loc=Local"
)

func TestMain(m *testing.M) {
	dsn := getTestDSN()
	testDBName = dbNameFromDSN(dsn)
	if testDBName == "" {
		fmt.Println("failed to parse database name from DSN")
		os.Exit(1)
	}

	var err error
	testDB, err = gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Printf("failed to connect to test database: %v\n", err)
		os.Exit(1)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		fmt.Printf("failed to get *sql.DB: %v\n", err)
		os.Exit(1)
	}

	driver, err := migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	if err != nil {
		sqlDB.Close()
		fmt.Printf("failed to create migrate driver: %v\n", err)
		os.Exit(1)
	}

	migrator, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "mysql", driver)
	if err != nil {
		sqlDB.Close()
		fmt.Printf("failed to create migrator: %v\n", err)
		os.Exit(1)
	}

	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		sqlDB.Close()
		fmt.Printf("failed to run migrations up: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := migrator.Down(); err != nil && err != migrate.ErrNoChange {
		fmt.Printf("warning: failed to run migrations down: %v\n", err)
	}
	sqlDB.Close()
	os.Exit(code)
}

func getTestDSN() string {
	if dsn := os.Getenv("TEST_DB_DSN"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

// dbNameFromDSN extracts the schema name from a DSN of the form
// user:pass@tcp(host:port)/dbname?params
func dbNameFromDSN(dsn string) string {
	start := strings.LastIndex(dsn, "/")
	if start == -1 {
		return ""
	}
	rest := dsn[start+1:]
	if end := strings.Index(rest, "?"); end != -1 {
		return rest[:end]
	}
	return rest
}

// TestTablesExist 验证 5 张表均存在
func TestTablesExist(t *testing.T) {
	tables := []string{"users", "tasks", "works", "credits", "login_attempts"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var count int64
			err := testDB.Raw(
				"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
				testDBName, table,
			).Scan(&count).Error
			require.NoError(t, err)
			assert.Equal(t, int64(1), count, "table %q should exist", table)
		})
	}
}

// TestUsersTableSchema 验证 users 表字段
func TestUsersTableSchema(t *testing.T) {
	type columnInfo struct {
		ColumnName string `gorm:"column:COLUMN_NAME"`
		DataType   string `gorm:"column:DATA_TYPE"`
		IsNullable string `gorm:"column:IS_NULLABLE"`
	}

	var columns []columnInfo
	err := testDB.Raw(`
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'users'
	`, testDBName).Scan(&columns).Error
	require.NoError(t, err)

	colMap := make(map[string]columnInfo)
	for _, c := range columns {
		colMap[c.ColumnName] = c
	}

	tests := []struct {
		column     string
		dataType   string
		isNullable string
	}{
		{"id", "bigint", "NO"},
		{"email", "varchar", "NO"},
		{"password", "varchar", "NO"},
		{"nickname", "varchar", "NO"},
		{"status", "tinyint", "NO"},
		{"created_at", "datetime", "NO"},
		{"updated_at", "datetime", "NO"},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			col, ok := colMap[tt.column]
			assert.True(t, ok, "column %q should exist in users table", tt.column)
			if ok {
				assert.Equal(t, tt.dataType, col.DataType, "column %q data type", tt.column)
				assert.Equal(t, tt.isNullable, col.IsNullable, "column %q nullable", tt.column)
			}
		})
	}
}

// TestTasksTableSchema 验证 tasks 表关键字段
func TestTasksTableSchema(t *testing.T) {
	var columns []struct {
		ColumnName string `gorm:"column:COLUMN_NAME"`
	}
	err := testDB.Raw(`
		SELECT COLUMN_NAME FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'tasks'
	`, testDBName).Scan(&columns).Error
	require.NoError(t, err)

	colSet := make(map[string]bool)
	for _, c := range columns {
		colSet[c.ColumnName] = true
	}

	required := []string{
		"id", "user_id", "prompt", "lyrics", "mode", "status",
		"fail_reason", "credit_date", "queue_position",
		"candidate_a_key", "candidate_b_key",
		"duration_seconds", "inference_ms",
		"started_at", "completed_at", "created_at", "updated_at",
	}
	for _, col := range required {
		assert.True(t, colSet[col], "column %q should exist in tasks table", col)
	}
}

// TestForeignKeys 验证外键约束
func TestForeignKeys(t *testing.T) {
	type fkInfo struct {
		ConstraintName string `gorm:"column:CONSTRAINT_NAME"`
		TableName      string `gorm:"column:TABLE_NAME"`
		ColumnName     string `gorm:"column:COLUMN_NAME"`
		RefTableName   string `gorm:"column:REFERENCED_TABLE_NAME"`
		RefColumnName  string `gorm:"column:REFERENCED_COLUMN_NAME"`
	}

	var fks []fkInfo
	err := testDB.Raw(`
		SELECT kcu.CONSTRAINT_NAME, kcu.TABLE_NAME, kcu.COLUMN_NAME,
		       kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE kcu
		JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
		  ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		  AND kcu.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
		WHERE kcu.TABLE_SCHEMA = ?
	`, testDBName).Scan(&fks).Error
	require.NoError(t, err)

	type fkKey struct{ table, column, refTable, refColumn string }
	fkSet := make(map[fkKey]bool)
	for _, fk := range fks {
		fkSet[fkKey{fk.TableName, fk.ColumnName, fk.RefTableName, fk.RefColumnName}] = true
	}

	expected := []fkKey{
		{"tasks", "user_id", "users", "id"},
		{"works", "user_id", "users", "id"},
		{"works", "task_id", "tasks", "id"},
		{"credits", "user_id", "users", "id"},
	}
	for _, e := range expected {
		assert.True(t, fkSet[e], "FK %s.%s → %s.%s should exist", e.table, e.column, e.refTable, e.refColumn)
	}
}

// TestIndexes 验证关键索引
func TestIndexes(t *testing.T) {
	type indexInfo struct {
		TableName  string `gorm:"column:TABLE_NAME"`
		IndexName  string `gorm:"column:INDEX_NAME"`
		ColumnName string `gorm:"column:COLUMN_NAME"`
	}

	var indexes []indexInfo
	err := testDB.Raw(`
		SELECT TABLE_NAME, INDEX_NAME, COLUMN_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ?
	`, testDBName).Scan(&indexes).Error
	require.NoError(t, err)

	type idxKey struct{ table, index string }
	idxSet := make(map[idxKey]bool)
	for _, idx := range indexes {
		idxSet[idxKey{idx.TableName, idx.IndexName}] = true
	}

	expected := []idxKey{
		{"users", "uk_email"},
		{"tasks", "idx_user_id_created"},
		{"tasks", "idx_status_started"},
		{"works", "idx_user_id_created"},
		{"works", "uk_task_id"},
		{"credits", "uk_user_date"},
		{"login_attempts", "idx_email_created"},
		{"login_attempts", "idx_ip_created"},
	}
	for _, e := range expected {
		assert.True(t, idxSet[e], "index %q on table %q should exist", e.index, e.table)
	}
}

// TestUniqueConstraints 验证唯一约束
func TestUniqueConstraints(t *testing.T) {
	// works.task_id 唯一约束：同一 task_id 不能保存两条作品
	// 先插入依赖数据
	tx := testDB.Exec(`INSERT INTO users (id, email, password, nickname) VALUES (9001, 'test@example.com', 'hash', 'test')`)
	require.NoError(t, tx.Error)
	defer testDB.Exec("DELETE FROM users WHERE id = 9001")

	tx = testDB.Exec(`INSERT INTO tasks (id, user_id, prompt, credit_date) VALUES (9001, 9001, 'test', '2026-01-01')`)
	require.NoError(t, tx.Error)
	defer testDB.Exec("DELETE FROM tasks WHERE id = 9001")

	// 第一次插入 work — 应该成功
	tx = testDB.Exec(`INSERT INTO works (id, user_id, task_id, title, prompt, mode, audio_key) VALUES (9001, 9001, 9001, 'T', 'p', 'vocal', 'k')`)
	require.NoError(t, tx.Error)
	defer testDB.Exec("DELETE FROM works WHERE id IN (9001, 9002)")

	// 第二次用相同 task_id — 应该违反唯一约束
	tx = testDB.Exec(`INSERT INTO works (id, user_id, task_id, title, prompt, mode, audio_key) VALUES (9002, 9001, 9001, 'T', 'p', 'vocal', 'k2')`)
	assert.Error(t, tx.Error, "duplicate task_id in works should fail")
}
