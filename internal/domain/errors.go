package domain

import (
	"errors"
	"fmt"
)

const (
	ErrBadRequest        = "ERR_BAD_REQUEST"
	ErrTaskNotFound      = "ERR_TASK_NOT_FOUND"
	ErrTaskNotRetryable  = "ERR_TASK_NOT_RETRYABLE"
	ErrTaskAlreadyFinal  = "ERR_TASK_ALREADY_FINAL"
	ErrFileTooLarge      = "ERR_FILE_TOO_LARGE"
	ErrUnsupportedMedia  = "ERR_UNSUPPORTED_MEDIA"
	ErrInternal          = "ERR_INTERNAL"
	ErrEmptyFile         = "ERR_EMPTY_FILE"
	ErrExtension         = "ERR_UNSUPPORTED_EXTENSION"
	ErrMIME              = "ERR_MIME_MISMATCH"
	ErrMagicMismatch     = "ERR_MAGIC_MISMATCH"
	ErrHashMismatch      = "ERR_HASH_MISMATCH"
	ErrExtract           = "ERR_EXTRACT_FAILED"
	ErrScan              = "ERR_SCAN_FAILED"
	ErrCallbackSignature = "ERR_INVALID_CALLBACK_SIGNATURE"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
func (e *Error) Unwrap() error { return e.Cause }
func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}
func WrapError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Code != "" {
		return target.Code
	}
	return ErrInternal
}
func ErrorMessage(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
