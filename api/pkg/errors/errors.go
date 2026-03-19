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
)
