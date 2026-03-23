package model

import "time"

// Work maps to the `works` table.
type Work struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement"`
	UserID          uint64     `gorm:"not null;index:idx_user_id_created,priority:1"`
	TaskID          uint64     `gorm:"not null;uniqueIndex:uk_task_id"`
	Title           string     `gorm:"size:50;not null"`
	Prompt          string     `gorm:"size:200;not null"`
	Mode            string     `gorm:"type:enum('vocal','instrumental');not null"`
	AudioKey        string     `gorm:"size:500;not null"`
	DurationSeconds int        `gorm:"not null;default:0"`
	PlayCount       uint       `gorm:"not null;default:0"`
	DeletedAt       *time.Time `gorm:"index"`
	CreatedAt       time.Time  `gorm:"not null;autoCreateTime;index:idx_user_id_created,priority:2"`
	UpdatedAt       time.Time  `gorm:"not null;autoUpdateTime"`
}
