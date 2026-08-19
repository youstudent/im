// Package err 定义统一的业务错误码与错误类型。
package err

import "fmt"

// Code 业务错误码。
type Code int

const (
	CodeOK           Code = 0
	CodeUnknown      Code = 10000
	CodeBadRequest   Code = 40000
	CodeUnauthorized Code = 40100
	CodeForbidden    Code = 40300
	CodeNotFound     Code = 40400
	CodeConflict     Code = 40900
	CodeTooMany      Code = 42900
	CodeInternal     Code = 50000
	CodeUnavailable  Code = 50300
)

// Error 业务错误，携带错误码，可在 HTTP 层直接序列化为统一响应。
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is / errors.As。
func (e *Error) Unwrap() error { return e.Cause }

// New 创建一个业务错误。
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Wrap 包装底层错误并附上业务错误码。
func Wrap(code Code, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}

// 便捷构造函数
func BadRequest(msg string) *Error       { return New(CodeBadRequest, msg) }
func Unauthorized(msg string) *Error     { return New(CodeUnauthorized, msg) }
func Forbidden(msg string) *Error        { return New(CodeForbidden, msg) }
func NotFound(msg string) *Error         { return New(CodeNotFound, msg) }
func Conflict(msg string) *Error        { return New(CodeConflict, msg) }
func TooManyRequests(msg string) *Error { return New(CodeTooMany, msg) }
func Internal(msg string) *Error         { return New(CodeInternal, msg) }
func Unavailable(msg string) *Error      { return New(CodeUnavailable, msg) }
func Unknown(msg string) *Error          { return New(CodeUnknown, msg) }
func WrapBadRequest(msg string, e error) *Error { return Wrap(CodeBadRequest, msg, e) }
func WrapInternal(msg string, e error) *Error   { return Wrap(CodeInternal, msg, e) }
