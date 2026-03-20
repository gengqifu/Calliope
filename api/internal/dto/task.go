package dto

import (
	"fmt"
	"time"
)

// ── 创建任务 ──────────────────────────────────────────────────────────────────

// CreateTaskRequest is the request body for POST /tasks.
type CreateTaskRequest struct {
	Prompt string  `json:"prompt" binding:"required,max=200"`
	Lyrics *string `json:"lyrics"  binding:"omitempty,max=3000"`
	Mode   string  `json:"mode"    binding:"omitempty,oneof=vocal instrumental"`
}

// CreateTaskResponse is the 202 response for POST /tasks.
type CreateTaskResponse struct {
	TaskID        uint64 `json:"task_id"`
	Status        string `json:"status"`         // always "queued"
	QueuePosition int    `json:"queue_position"` // number of tasks ahead
	Message       string `json:"message"`
}

// ── 查询任务 ──────────────────────────────────────────────────────────────────

// AudioCandidate represents one generated audio candidate.
type AudioCandidate struct {
	Index           string `json:"index"` // "a" or "b"
	URL             string `json:"url"`   // 七牛云签名临时 URL，有效期 1 小时
	DurationSeconds int    `json:"duration_seconds"`
}

// TaskDetailResponse is the 200 response for GET /tasks/{task_id}.
type TaskDetailResponse struct {
	TaskID          uint64           `json:"task_id"`
	Status          string           `json:"status"`
	Prompt          string           `json:"prompt"`
	Lyrics          *string          `json:"lyrics,omitempty"`
	Mode            string           `json:"mode"`
	QueuePosition   *int             `json:"queue_position,omitempty"`   // status=queued
	Progress        *int             `json:"progress,omitempty"`         // status=processing, 0-90
	Candidates      []AudioCandidate `json:"candidates,omitempty"`       // status=completed
	FailReason      *string          `json:"fail_reason,omitempty"`      // status=failed
	DurationSeconds *int             `json:"duration_seconds,omitempty"` // status=completed
	CreatedAt       time.Time        `json:"created_at"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
}

// ── 内部回调 ──────────────────────────────────────────────────────────────────

// UpdateTaskStatusRequest is the body for POST /internal/tasks/{task_id}/status.
type UpdateTaskStatusRequest struct {
	Status          string  `json:"status"           binding:"required,oneof=processing completed failed"`
	FailReason      *string `json:"fail_reason"`
	CandidateAKey   *string `json:"candidate_a_key"`
	CandidateBKey   *string `json:"candidate_b_key"`
	DurationSeconds *int    `json:"duration_seconds"`
	InferenceMs     *int    `json:"inference_ms"`
}

// Validate enforces conditional required fields that cannot be expressed with
// struct tags alone:
//
//	status=completed → candidate_a_key, candidate_b_key, duration_seconds 必填
//	status=failed    → fail_reason 必填，且只能是 timeout|inference_error|upload_error
func (r UpdateTaskStatusRequest) Validate() error {
	switch r.Status {
	case "completed":
		if r.CandidateAKey == nil || *r.CandidateAKey == "" {
			return fmt.Errorf("candidate_a_key is required when status=completed")
		}
		if r.CandidateBKey == nil || *r.CandidateBKey == "" {
			return fmt.Errorf("candidate_b_key is required when status=completed")
		}
		if r.DurationSeconds == nil {
			return fmt.Errorf("duration_seconds is required when status=completed")
		}
	case "failed":
		if r.FailReason == nil || *r.FailReason == "" {
			return fmt.Errorf("fail_reason is required when status=failed")
		}
		switch *r.FailReason {
		case "timeout", "inference_error", "upload_error":
			// valid
		default:
			return fmt.Errorf("fail_reason must be one of: timeout, inference_error, upload_error")
		}
	}
	return nil
}
