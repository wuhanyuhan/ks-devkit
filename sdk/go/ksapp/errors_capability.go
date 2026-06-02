package ksapp

import (
	"errors"
	"fmt"
)

// Capability Mesh 错误层级。
//
// 设计：sentinel error + 具体错误结构体两层。判定大类用 errors.Is(err, ErrXxxKind)；
// 拿具体上下文（canonical_name / retry_after 等）用 errors.As(err, &XxxErr{})。
//
// 与现有 errors.go（SDK 通用错误，如 keystoneclient.ErrFetchFailed）并列。

// ── sentinel hierarchy ────────────────────────────────────────────────────────

var (
	ErrKeystoneError = errors.New("keystone error")

	ErrAuthError             = fmt.Errorf("auth error: %w", ErrKeystoneError)
	ErrTokenInvalid          = fmt.Errorf("token invalid: %w", ErrAuthError)
	ErrTokenExpired          = fmt.Errorf("token expired: %w", ErrAuthError)
	ErrTokenAudienceMismatch = fmt.Errorf("token aud mismatch: %w", ErrAuthError)

	ErrPermissionError     = fmt.Errorf("permission error: %w", ErrKeystoneError)
	ErrCapabilityForbidden = fmt.Errorf("capability forbidden: %w", ErrPermissionError)
	ErrApprovalRequired    = fmt.Errorf("approval required: %w", ErrPermissionError)
	ErrCapabilityDisabled  = fmt.Errorf("capability disabled: %w", ErrPermissionError)

	ErrNotFoundError      = fmt.Errorf("not found: %w", ErrKeystoneError)
	ErrCapabilityNotFound = fmt.Errorf("capability not found: %w", ErrNotFoundError)
	ErrTaskNotFound       = fmt.Errorf("task not found: %w", ErrNotFoundError)

	ErrValidationError  = fmt.Errorf("validation error: %w", ErrKeystoneError)
	ErrInvalidArgs      = fmt.Errorf("invalid args: %w", ErrValidationError)
	ErrManifestMismatch = fmt.Errorf("manifest mismatch: %w", ErrValidationError)

	ErrDependencyError       = fmt.Errorf("dependency error: %w", ErrKeystoneError)
	ErrCapabilityUnavailable = fmt.Errorf("capability unavailable: %w", ErrDependencyError)
	ErrLoopDetected          = fmt.Errorf("loop detected: %w", ErrDependencyError)
	ErrGuardrailBlocked      = fmt.Errorf("guardrail blocked: %w", ErrDependencyError)

	ErrExecutionError      = fmt.Errorf("execution error: %w", ErrKeystoneError)
	ErrBackendError        = fmt.Errorf("backend error: %w", ErrExecutionError)
	ErrTimeout             = fmt.Errorf("capability timeout: %w", ErrExecutionError)
	ErrCancelled           = fmt.Errorf("capability cancelled: %w", ErrExecutionError)
	ErrDispatcherRestarted = fmt.Errorf("dispatcher restarted: %w", ErrExecutionError)

	ErrRateLimitError             = fmt.Errorf("rate limit: %w", ErrKeystoneError)
	ErrCapabilityConcurrencyLimit = fmt.Errorf("capability concurrency limit: %w", ErrRateLimitError)
	ErrUserQuotaExceeded          = fmt.Errorf("user quota exceeded: %w", ErrRateLimitError)
	ErrAppQuotaExceeded           = fmt.Errorf("app quota exceeded: %w", ErrRateLimitError)
)

// ── 携带上下文的具体错误结构体 ───────────────────────────────────────────────

// CapabilityNotFoundErr 携带 canonical_name。
type CapabilityNotFoundErr struct {
	CanonicalName string
	Message       string
}

func (e *CapabilityNotFoundErr) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("capability not found: %s", e.CanonicalName)
}

func (e *CapabilityNotFoundErr) Unwrap() error { return ErrCapabilityNotFound }

func NewCapabilityNotFound(canonicalName string) error {
	return &CapabilityNotFoundErr{CanonicalName: canonicalName}
}

// TaskNotFoundErr 携带 task_id。
type TaskNotFoundErr struct {
	TaskID  string
	Message string
}

func (e *TaskNotFoundErr) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("task not found: %s", e.TaskID)
}

func (e *TaskNotFoundErr) Unwrap() error { return ErrTaskNotFound }

func NewTaskNotFound(taskID string) error {
	return &TaskNotFoundErr{TaskID: taskID}
}

// TokenAudienceMismatchErr 携带具体信息（aud expected/got）。
type TokenAudienceMismatchErr struct{ Message string }

func (e *TokenAudienceMismatchErr) Error() string { return "token aud mismatch: " + e.Message }
func (e *TokenAudienceMismatchErr) Unwrap() error { return ErrTokenAudienceMismatch }

func NewTokenAudienceMismatch(message string) error {
	return &TokenAudienceMismatchErr{Message: message}
}

// ManifestMismatchErr 携带具体注册名 + manifest 已知名列表。
type ManifestMismatchErr struct {
	Registered    string
	ManifestNames []string
}

func (e *ManifestMismatchErr) Error() string {
	return fmt.Sprintf("capability %q not in manifest.provides.capabilities; manifest_names=%v",
		e.Registered, e.ManifestNames)
}
func (e *ManifestMismatchErr) Unwrap() error { return ErrManifestMismatch }

func NewManifestMismatch(registered string, manifestNames []string) error {
	return &ManifestMismatchErr{Registered: registered, ManifestNames: manifestNames}
}

// CapabilityUnavailableErr 携带 retry_after_ms。
type CapabilityUnavailableErr struct {
	Message      string
	RetryAfterMs int
}

func (e *CapabilityUnavailableErr) Error() string { return e.Message }
func (e *CapabilityUnavailableErr) Unwrap() error { return ErrCapabilityUnavailable }

func NewCapabilityUnavailable(message string, retryAfterMs int) error {
	return &CapabilityUnavailableErr{Message: message, RetryAfterMs: retryAfterMs}
}

// RateLimitErr 携带 retry_after_ms（用于 429）。
// Sentinel 字段指向具体子类（CapabilityConcurrencyLimit / UserQuotaExceeded / AppQuotaExceeded）。
type RateLimitErr struct {
	Message      string
	RetryAfterMs int
	Sentinel     error
}

func (e *RateLimitErr) Error() string { return e.Message }
func (e *RateLimitErr) Unwrap() error {
	if e.Sentinel != nil {
		return e.Sentinel
	}
	return ErrRateLimitError
}

// TimeoutErr 携带 deadline / elapsed（毫秒）。
type TimeoutErr struct {
	DeadlineMs int64
	ElapsedMs  int64
}

func (e *TimeoutErr) Error() string {
	return fmt.Sprintf("capability timeout: elapsed=%dms deadline=%dms", e.ElapsedMs, e.DeadlineMs)
}
func (e *TimeoutErr) Unwrap() error { return ErrTimeout }
