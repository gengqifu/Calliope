package model

import "time"

// User maps to the `users` table.
type User struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	Email     string    `gorm:"size:100;uniqueIndex;not null"`
	Password  string    `gorm:"size:60;not null"` // bcrypt hash, always 60 chars
	Nickname  string    `gorm:"size:50;not null;default:''"`
	Status    int8      `gorm:"not null;default:1"` // 1=active, 0=banned
	CreatedAt time.Time `gorm:"not null;autoCreateTime"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"`
}
