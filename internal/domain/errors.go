package domain

import "errors"

// The structured error type below is derived from rhizome-mcp
// (https://github.com/Odrin/rhizome-mcp), via boboty/agent-board-rhizome-poc,
// licensed under Apache-2.0. See NOTICE. The error codes are Agent Board's own.

const (
	// CodeInvalidArgument identifies invalid caller input.
	CodeInvalidArgument = "INVALID_ARGUMENT"
	// CodeTaskNotFound identifies a task reference that is not present.
	CodeTaskNotFound = "TASK_NOT_FOUND"
	// CodeVersionConflict identifies a failed optimistic task version precondition.
	CodeVersionConflict = "VERSION_CONFLICT"
	// CodeReadyOrderConflict identifies a READY reorder whose expected READY
	// version or task set no longer matches the stored READY queue.
	CodeReadyOrderConflict = "READY_ORDER_CONFLICT"
	// CodeTaskNotQueued identifies a state request for a task that has no
	// lifecycle state yet because it has not been queued.
	CodeTaskNotQueued = "TASK_NOT_QUEUED"
	// CodeAlreadyQueued identifies a queue request for a task that is already queued.
	CodeAlreadyQueued = "ALREADY_QUEUED"
	// CodeIdempotencyConflict identifies reuse of an idempotency key with a
	// different normalized request.
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	// CodeProjectMismatch identifies a database bound to a different project.
	CodeProjectMismatch = "PROJECT_MISMATCH"
	// CodeIDGeneration identifies failure to generate an identifier.
	CodeIDGeneration = "ID_GENERATION_FAILED"

	// CodeStorageBusy identifies exhausted SQLite lock-contention retries.
	CodeStorageBusy = "STORAGE_BUSY"
	// CodeStorageUnavailable identifies inaccessible or failed storage.
	CodeStorageUnavailable = "STORAGE_UNAVAILABLE"
	// CodeStorageCorrupt identifies corrupt or non-database SQLite files.
	CodeStorageCorrupt = "STORAGE_CORRUPT"
	// CodeStorageConfiguration identifies an invalid or unsupported storage setup.
	CodeStorageConfiguration = "STORAGE_CONFIGURATION"
	// CodeStorageConstraint identifies a database constraint violation.
	CodeStorageConstraint = "STORAGE_CONSTRAINT"
	// CodeStorageMigration identifies invalid migration history or schema migration failure.
	CodeStorageMigration = "STORAGE_MIGRATION"
	// CodeStorageFailure identifies another SQLite operation failure.
	CodeStorageFailure = "STORAGE_FAILURE"
)

// Detail is one stable, field-oriented error detail.
type Detail struct {
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Error is a stable structured error suitable for adapter mapping.
type Error struct {
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Details   []Detail `json:"details"`
	Retryable bool     `json:"retryable"`
	cause     error
}

// NewError constructs an Error.
func NewError(code, message string, retryable bool, details ...Detail) *Error {
	ordered := append([]Detail{}, details...)
	return &Error{Code: code, Message: message, Details: ordered, Retryable: retryable}
}

// WrapError constructs an Error that unwraps to cause.
func WrapError(cause error, code, message string, retryable bool, details ...Detail) *Error {
	err := NewError(code, message, retryable, details...)
	err.cause = cause
	return err
}

// Invalid returns an INVALID_ARGUMENT error for one field.
func Invalid(field, message string) *Error {
	return NewError(CodeInvalidArgument, field+": "+message, false, Detail{Field: field, Code: CodeInvalidArgument, Message: message})
}

// Error returns the stable human-readable message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap returns the internal cause, when one was supplied.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is matches another Error by non-empty stable code, so callers can test
// errors.Is(err, &domain.Error{Code: domain.CodeVersionConflict}).
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && other.Code != "" && e.Code == other.Code
}

// IsCode reports whether err is or wraps an Error with code.
func IsCode(err error, code string) bool {
	return errors.Is(err, &Error{Code: code})
}
