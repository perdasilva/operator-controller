package deppy

import "fmt"

type RetryableError string

func (v RetryableError) Error() string {
	return string(v)
}

func RetryableErrorf(format string, args ...interface{}) RetryableError {
	return RetryableError(fmt.Sprintf(format, args...))
}

type FatalError string

func (v FatalError) Error() string {
	return string(v)
}

func Fatalf(format string, args ...interface{}) FatalError {
	return FatalError(fmt.Sprintf(format, args...))
}

type ConflictError FatalError

func (v ConflictError) Error() string {
	return string(v)
}

func ConflictErrorf(format string, args ...interface{}) ConflictError {
	return ConflictError(fmt.Sprintf(format, args...))
}

type NotFoundError RetryableError

func (v NotFoundError) Error() string {
	return fmt.Sprintf("variable with id %s not found", string(v))
}

func NotFoundErrorf(format string, args ...interface{}) NotFoundError {
	return NotFoundError(fmt.Sprintf(format, args...))
}

type PreconditionError RetryableError

func (v PreconditionError) Error() string {
	return fmt.Sprintf("precondition failed: %s", string(v))
}

func PreconditionErrorf(format string, args ...interface{}) PreconditionError {
	return PreconditionError(fmt.Sprintf(format, args...))
}

func IsConflictError(err error) bool {
	_, ok := err.(ConflictError)
	return ok
}

func IsPreconditionError(err error) bool {
	_, ok := err.(PreconditionError)
	return ok
}

func IsNotFoundError(err error) bool {
	_, ok := err.(NotFoundError)
	return ok
}

func IgnoreNotFound(err error) error {
	if IsNotFoundError(err) {
		return nil
	}
	return err
}

func IsRetryableError(err error) bool {
	_, ok := err.(RetryableError)
	return ok
}

func IsFatalError(err error) bool {
	_, ok := err.(FatalError)
	return ok
}
