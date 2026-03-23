package dto

import "time"

// ── 保存作品 ──────────────────────────────────────────────────────────────────

// SaveWorkRequest is the request body for POST /works.
type SaveWorkRequest struct {
	TaskID    uint64 `json:"task_id"   binding:"required"`
	Candidate string `json:"candidate" binding:"required,oneof=a b"`
	Title     string `json:"title"     binding:"required,max=50"`
}

// ── 作品响应 ──────────────────────────────────────────────────────────────────

// WorkResponse is the standard work representation returned by API responses.
type WorkResponse struct {
	ID                uint64    `json:"id"`
	Title             string    `json:"title"`
	Prompt            string    `json:"prompt"`
	Mode              string    `json:"mode"`
	AudioURL          string    `json:"audio_url"`
	AudioURLExpiresAt time.Time `json:"audio_url_expires_at"`
	DurationSeconds   int       `json:"duration_seconds"`
	PlayCount         uint      `json:"play_count"`
	CreatedAt         time.Time `json:"created_at"`
}

// ── 列表查询 ──────────────────────────────────────────────────────────────────

// ListWorksRequest holds pagination parameters for GET /works.
type ListWorksRequest struct {
	Page     int `form:"page"      binding:"omitempty,min=1"`
	PageSize int `form:"page_size" binding:"omitempty,min=1,max=50"`
}

// ListWorksResponse is the paginated response for GET /works.
type ListWorksResponse struct {
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []WorkResponse `json:"items"`
}

// ── 下载 URL ──────────────────────────────────────────────────────────────────

// DownloadURLResponse is the response for GET /works/{work_id}/download.
type DownloadURLResponse struct {
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	ExpiresIn   int    `json:"expires_in"` // 秒
}
