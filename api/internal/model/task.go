package model

import "time"

// Task maps to the `tasks` table.
type Task struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement"`
	UserID          uint64     `gorm:"not null;index:idx_user_id_created,priority:1"`
	Prompt          string     `gorm:"size:200;not null"`
	Lyrics          *string    `gorm:"type:text"`
	Mode            string     `gorm:"type:enum('vocal','instrumental');not null;default:vocal"`
	Status          string     `gorm:"type:enum('queued','processing','completed','failed');not null;default:queued"`
	FailReason      *string    `gorm:"size:500"`
	CreditDate      string     `gorm:"type:date;not null"` // UTC+8 日期，应用层写入
	QueuePosition   *int       `gorm:""`
	CandidateAKey   *string    `gorm:"size:500"`
	CandidateBKey   *string    `gorm:"size:500"`
	DurationSeconds *int       `gorm:""`
	InferenceMs     *int       `gorm:""`
	StartedAt       *time.Time `gorm:""`
	CompletedAt     *time.Time `gorm:""`
	CreatedAt       time.Time  `gorm:"not null;autoCreateTime;index:idx_user_id_created,priority:2"`
	UpdatedAt       time.Time  `gorm:"not null;autoUpdateTime"`
}

// Credit maps to the `credits` table.
type Credit struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	UserID     uint64    `gorm:"not null;uniqueIndex:uk_user_date,priority:1"`
	Date       string    `gorm:"type:date;not null;uniqueIndex:uk_user_date,priority:2"`
	Used       int8      `gorm:"not null;default:0"`
	LimitCount int8      `gorm:"not null;default:5"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"not null;autoUpdateTime"`
}
