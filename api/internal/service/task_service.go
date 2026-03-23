package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/calliope/api/internal/dto"
	"github.com/calliope/api/internal/model"
	"github.com/calliope/api/internal/repository"
	apierrors "github.com/calliope/api/pkg/errors"
)

// Lua script: atomically XADD to the stream and INCR the depth counter.
// KEYS[1] = stream key, KEYS[2] = depth key
// ARGV[1..5] = task_id, user_id, prompt, mode, created_at
// ARGV[6]    = queue_depth_max (hard ceiling enforced inside Lua)
//
// Returns {streamID, newDepth} on success, or redis.error_reply("QUEUE_FULL")
// if the post-INCR depth exceeds the limit. This closes the TOCTOU window
// between the pre-check GET and the actual INCR.
// XaddIncrScript is exported so integration tests can reference the exact same
// script rather than maintaining a copy that could silently drift.
const XaddIncrScript = `
local depth = redis.call('INCR', KEYS[2])
if depth > tonumber(ARGV[6]) then
    redis.call('DECR', KEYS[2])
    return redis.error_reply('QUEUE_FULL')
end
local streamID = redis.call('XADD', KEYS[1], 'MAXLEN', '~', '10000', '*',
    'task_id', ARGV[1], 'user_id', ARGV[2], 'prompt', ARGV[3], 'mode', ARGV[4], 'created_at', ARGV[5])
return {streamID, depth}
`

const (
	redisStreamKey = "calliope:tasks:stream"
	redisDepthKey  = "calliope:queue:depth"
	redisWsPrefix  = "calliope:ws:task:"
)

// OSSSignerClient is the subset of OSS operations required by TaskService.
type OSSSignerClient interface {
	SignURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// TaskServiceConfig holds all configuration needed by TaskService.
type TaskServiceConfig struct {
	QueueDepthMax        int
	TaskTimeoutSec       int
	SignedURLTTL         time.Duration
	ExpectedInferenceSec int
}

// TaskService defines the task management business logic.
type TaskService interface {
	CreateTask(ctx context.Context, userID uint64, req dto.CreateTaskRequest) (*dto.CreateTaskResponse, error)
	GetTask(ctx context.Context, userID uint64, taskID uint64) (*dto.TaskDetailResponse, error)
	UpdateTaskStatus(ctx context.Context, taskID uint64, req dto.UpdateTaskStatusRequest) error
	FixTimedOutTasks(ctx context.Context) error
}

type taskServiceImpl struct {
	cfg        TaskServiceConfig
	taskRepo   repository.TaskRepository
	creditRepo repository.CreditRepository
	rdb        RedisClient
	oss        OSSSignerClient
}

// NewTaskService creates a new TaskService.
func NewTaskService(
	cfg TaskServiceConfig,
	taskRepo repository.TaskRepository,
	creditRepo repository.CreditRepository,
	rdb RedisClient,
	oss OSSSignerClient,
) TaskService {
	return &taskServiceImpl{cfg: cfg, taskRepo: taskRepo, creditRepo: creditRepo, rdb: rdb, oss: oss}
}

// ── CreateTask ────────────────────────────────────────────────────────────────

func (s *taskServiceImpl) CreateTask(ctx context.Context, userID uint64, req dto.CreateTaskRequest) (*dto.CreateTaskResponse, error) {
	// Step 1: queue depth gate
	depthStr, err := s.rdb.Get(ctx, redisDepthKey)
	if err == nil {
		depth, _ := strconv.Atoi(depthStr)
		if depth >= s.cfg.QueueDepthMax {
			return nil, apierrors.ErrQueueFull
		}
	}

	// Step 2: deduct daily credit
	date := creditDate()
	if err := s.creditRepo.TryDeduct(ctx, userID, date); err != nil {
		return nil, err
	}

	// Normalize mode default
	mode := req.Mode
	if mode == "" {
		mode = "vocal"
	}

	// Step 3: insert task into DB
	task := &model.Task{
		UserID:     userID,
		Prompt:     req.Prompt,
		Lyrics:     req.Lyrics,
		Mode:       mode,
		Status:     "queued",
		CreditDate: date,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		// Rollback: refund credit
		_ = s.creditRepo.Refund(ctx, userID, date)
		return nil, fmt.Errorf("taskService.CreateTask: create task: %w", err)
	}

	// Step 4: atomically INCR depth + XADD via Lua (hard ceiling enforced in script)
	result, err := s.rdb.Eval(ctx, XaddIncrScript,
		[]string{redisStreamKey, redisDepthKey},
		strconv.FormatUint(task.ID, 10),
		strconv.FormatUint(userID, 10),
		req.Prompt,
		mode,
		task.CreatedAt.UTC().Format(time.RFC3339),
		strconv.Itoa(s.cfg.QueueDepthMax),
	)
	if err != nil {
		// Rollback: refund credit + mark task failed
		_ = s.creditRepo.Refund(ctx, userID, date)
		_ = s.taskRepo.UpdateStatus(ctx, task.ID, map[string]interface{}{
			"status":       "failed",
			"fail_reason":  "入队失败",
			"completed_at": time.Now(),
		})
		// Lua returns redis.error_reply("QUEUE_FULL") when limit exceeded
		if err.Error() == "QUEUE_FULL" {
			return nil, apierrors.ErrQueueFull
		}
		return nil, fmt.Errorf("taskService.CreateTask: enqueue: %w", err)
	}

	// Extract queue position from Lua result [streamID, newDepth]
	queuePosition := 0
	if arr, ok := result.([]interface{}); ok && len(arr) == 2 {
		if depth, ok := arr[1].(int64); ok && depth > 1 {
			queuePosition = int(depth) - 1
		}
	}

	// Persist queue_position in DB (best-effort)
	_ = s.taskRepo.UpdateStatus(ctx, task.ID, map[string]interface{}{"queue_position": queuePosition})

	return &dto.CreateTaskResponse{
		TaskID:        task.ID,
		Status:        "queued",
		QueuePosition: queuePosition,
		Message:       fmt.Sprintf("任务已提交，前方还有 %d 个任务", queuePosition),
	}, nil
}

// ── GetTask ───────────────────────────────────────────────────────────────────

func (s *taskServiceImpl) GetTask(ctx context.Context, userID uint64, taskID uint64) (*dto.TaskDetailResponse, error) {
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, apierrors.ErrForbidden
	}

	resp := &dto.TaskDetailResponse{
		TaskID:      task.ID,
		Status:      task.Status,
		Prompt:      task.Prompt,
		Lyrics:      task.Lyrics,
		Mode:        task.Mode,
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.CompletedAt,
	}

	switch task.Status {
	case "queued":
		resp.QueuePosition = task.QueuePosition

	case "processing":
		if task.StartedAt != nil {
			p := pseudoProgress(*task.StartedAt, time.Duration(s.cfg.ExpectedInferenceSec)*time.Second)
			resp.Progress = &p
		}

	case "completed":
		resp.DurationSeconds = task.DurationSeconds
		dur := 0
		if task.DurationSeconds != nil {
			dur = *task.DurationSeconds
		}
		if task.CandidateAKey != nil {
			url, err := s.oss.SignURL(ctx, *task.CandidateAKey, s.cfg.SignedURLTTL)
			if err != nil {
				return nil, fmt.Errorf("taskService.GetTask: sign candidate_a: %w", err)
			}
			resp.Candidates = append(resp.Candidates, dto.AudioCandidate{Index: "a", URL: url, DurationSeconds: dur})
		}
		if task.CandidateBKey != nil {
			url, err := s.oss.SignURL(ctx, *task.CandidateBKey, s.cfg.SignedURLTTL)
			if err != nil {
				return nil, fmt.Errorf("taskService.GetTask: sign candidate_b: %w", err)
			}
			resp.Candidates = append(resp.Candidates, dto.AudioCandidate{Index: "b", URL: url, DurationSeconds: dur})
		}

	case "failed":
		resp.FailReason = task.FailReason
	}

	return resp, nil
}

// ── UpdateTaskStatus ──────────────────────────────────────────────────────────

// CallbackConflictError is returned when the Worker's status transition is
// rejected because the task is already in a conflicting state.
// Reason is "duplicate" (already at target) or "invalid_transition" (wrong state).
type CallbackConflictError struct {
	Reason        string
	CurrentStatus string
}

func (e *CallbackConflictError) Error() string {
	return fmt.Sprintf("callback conflict: reason=%s current_status=%s", e.Reason, e.CurrentStatus)
}

// validFromStatuses returns the set of allowed predecessor statuses for a given
// target, per the internal-protocol spec §3.4:
//
//	processing → FROM queued only
//	completed  → FROM processing only
//	failed     → FROM queued OR processing (worker may abort a queued task)
func validFromStatuses(target string) ([]string, bool) {
	switch target {
	case "processing":
		return []string{"queued"}, true
	case "completed":
		return []string{"processing"}, true
	case "failed":
		return []string{"queued", "processing"}, true
	}
	return nil, false
}

func (s *taskServiceImpl) UpdateTaskStatus(ctx context.Context, taskID uint64, req dto.UpdateTaskStatusRequest) error {
	fromStatuses, ok := validFromStatuses(req.Status)
	if !ok {
		return apierrors.ErrInvalidTransition
	}

	fields := map[string]interface{}{"status": req.Status}

	switch req.Status {
	case "processing":
		fields["started_at"] = time.Now()

	case "completed":
		now := time.Now()
		fields["completed_at"] = now
		if req.CandidateAKey != nil {
			fields["candidate_a_key"] = *req.CandidateAKey
		}
		if req.CandidateBKey != nil {
			fields["candidate_b_key"] = *req.CandidateBKey
		}
		if req.DurationSeconds != nil {
			fields["duration_seconds"] = *req.DurationSeconds
		}
		if req.InferenceMs != nil {
			fields["inference_ms"] = *req.InferenceMs
		}

	case "failed":
		fields["completed_at"] = time.Now()
		if req.FailReason != nil {
			fields["fail_reason"] = *req.FailReason
		}
	}

	updated, err := s.taskRepo.ConditionalUpdateStatus(ctx, taskID, fields, fromStatuses)
	if err != nil {
		return fmt.Errorf("taskService.UpdateTaskStatus: %w", err)
	}

	if !updated {
		// No rows matched WHERE id=? AND status IN (?). Fetch current state to
		// distinguish duplicate (already at target) from invalid_transition.
		task, err := s.taskRepo.FindByID(ctx, taskID)
		if err != nil {
			return err // e.g. ErrNotFound
		}
		reason := "invalid_transition"
		if task.Status == req.Status {
			reason = "duplicate"
		}
		return &CallbackConflictError{Reason: reason, CurrentStatus: task.Status}
	}

	// For terminal states: DECR depth + PUBLISH WebSocket notification.
	// On failed: also refund the daily credit (load task for UserID/CreditDate).
	if req.Status == "completed" || req.Status == "failed" {
		if req.Status == "failed" {
			if task, err := s.taskRepo.FindByID(ctx, taskID); err == nil {
				_ = s.creditRepo.Refund(ctx, task.UserID, task.CreditDate)
			}
		}
		_, _ = s.rdb.Decr(ctx, redisDepthKey)
		_ = s.publishWSMessage(ctx, taskID, req.Status, req.FailReason)
	}

	return nil
}

// ── FixTimedOutTasks ──────────────────────────────────────────────────────────

func (s *taskServiceImpl) FixTimedOutTasks(ctx context.Context) error {
	cutoff := time.Now().Add(-time.Duration(s.cfg.TaskTimeoutSec) * time.Second)
	tasks, err := s.taskRepo.FindTimedOutProcessing(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("taskService.FixTimedOutTasks: find: %w", err)
	}

	timeoutReason := "推理超时"
	for _, task := range tasks {
		// Guard against races: if the worker completed the task between
		// FindTimedOutProcessing and here, ConditionalUpdateStatus will not
		// match (status is no longer "processing") and we skip side-effects.
		updated, err := s.taskRepo.ConditionalUpdateStatus(ctx, task.ID, map[string]interface{}{
			"status":       "failed",
			"fail_reason":  timeoutReason,
			"completed_at": time.Now(),
		}, []string{"processing"})
		if err != nil || !updated {
			continue
		}
		_ = s.creditRepo.Refund(ctx, task.UserID, task.CreditDate)
		_, _ = s.rdb.Decr(ctx, redisDepthKey)
		_ = s.publishWSMessage(ctx, task.ID, "failed", &timeoutReason)
	}
	return nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

type wsMessage struct {
	TaskID     uint64  `json:"task_id"`
	Status     string  `json:"status"`
	FailReason *string `json:"fail_reason,omitempty"`
}

func (s *taskServiceImpl) publishWSMessage(ctx context.Context, taskID uint64, status string, failReason *string) error {
	msg := wsMessage{TaskID: taskID, Status: status, FailReason: failReason}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("publishWSMessage: marshal: %w", err)
	}
	channel := fmt.Sprintf("%s%d", redisWsPrefix, taskID)
	return s.rdb.Publish(ctx, channel, string(payload))
}

// creditDate returns the current date in UTC+8, as "YYYY-MM-DD".
// Must NOT use DB CURDATE() — MySQL default TZ is UTC, causing wrong date 0–8 AM CST.
func creditDate() string {
	cst := time.FixedZone("CST", 8*3600)
	return time.Now().In(cst).Format("2006-01-02")
}

// pseudoProgress returns an estimated progress 0–90 based on elapsed time.
// Maximum is 90; reaching 100 is reserved for the completed status.
func pseudoProgress(startedAt time.Time, expected time.Duration) int {
	elapsed := time.Since(startedAt)
	if expected <= 0 {
		return 0
	}
	p := int(float64(elapsed) / float64(expected) * 90)
	if p > 90 {
		return 90
	}
	if p < 0 {
		return 0
	}
	return p
}

