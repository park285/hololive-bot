package transport

import (
	"errors"
)

var ErrReplyOutcomeUnknown = errors.New("iris reply outcome unknown")

type replyOutcomeUnknownError struct {
	reason string
	cause  error
}

func (e replyOutcomeUnknownError) Error() string {
	message := "iris reply outcome unknown: " + e.reason
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}

	return message
}

func (e replyOutcomeUnknownError) Unwrap() error { return e.cause }

func (e replyOutcomeUnknownError) Is(target error) bool { return target == ErrReplyOutcomeUnknown }

func IsReplyOutcomeUnknown(err error) bool {
	return errors.Is(err, ErrReplyOutcomeUnknown) || errors.Is(err, ErrReplyStagingFailed)
}
