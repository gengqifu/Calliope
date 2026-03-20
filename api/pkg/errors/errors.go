package errors

// AppError is the standard error type for business-level errors.
// Fields are unexported to prevent external mutation of shared instances.
type AppError struct {
	code    string
	message string
	err     error
}

func (e *AppError) Error() string  { return e.message }
func (e *AppError) Unwrap() error  { return e.err }
func (e *AppError) Code() string   { return e.code }
func (e *AppError) Message() string { return e.message }

// New creates an AppError without an underlying cause.
func New(code, message string) *AppError {
	return &AppError{code: code, message: message}
}

// Wrap creates an AppError wrapping an underlying error.
func Wrap(code, message string, err error) *AppError {
	return &AppError{code: code, message: message, err: err}
}

// Predefined sentinel errors — treat as read-only; use errors.Is() for matching.
var (
	ErrNotFound     = New("NOT_FOUND", "资源不存在")
	ErrUnauthorized = New("UNAUTHORIZED", "未授权，请先登录")
	ErrForbidden    = New("FORBIDDEN", "无权限执行此操作")
	ErrInternal     = New("INTERNAL_ERROR", "服务器内部错误")

	// Auth-specific errors
	ErrPasswordMismatch      = New("PASSWORD_MISMATCH", "两次输入的密码不一致")
	ErrEmailAlreadyExists    = New("EMAIL_ALREADY_EXISTS", "该邮箱已注册")
	ErrInvalidCredentials    = New("INVALID_CREDENTIALS", "邮箱或密码不正确")
	ErrAccountLocked         = New("ACCOUNT_LOCKED", "登录失败次数过多，账号已锁定 15 分钟")
	ErrInvalidRefreshToken   = New("INVALID_REFRESH_TOKEN", "Refresh Token 无效或已过期")

	// Task-specific errors
	ErrInsufficientCredits = New("INSUFFICIENT_CREDITS", "今日额度已用完，明天再来")
	ErrQueueFull           = New("QUEUE_FULL", "当前生成队列已满，请稍后再试")
	ErrContentFiltered     = New("CONTENT_FILTERED", "输入内容包含违禁词汇，请修改后重试")
	ErrTaskNotCompleted    = New("TASK_NOT_COMPLETED", "任务尚未完成，无法保存")
	ErrInvalidTransition   = New("INVALID_STATUS_TRANSITION", "当前任务状态不允许此操作")
)
