package durability

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
)

type repositoryError struct {
	operation    string
	messageToken string
	reason       string
	cause        error
}

func (e *repositoryError) Error() string {
	if e.messageToken == "" {
		return fmt.Sprintf("%s: reason=%s", e.operation, e.reason)
	}

	return fmt.Sprintf("%s: message_token=%s reason=%s", e.operation, e.messageToken, e.reason)
}

func (e *repositoryError) Unwrap() error {
	return e.cause
}

func (e *repositoryError) SafeMessageToken() string {
	return e.messageToken
}

func (e *repositoryError) SafeReason() string {
	return e.reason
}

func safeRepositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return &repositoryError{operation: operation, reason: repositoryErrorReason(err), cause: err}
}

func safeMessageRepositoryError(operation, messageID string, err error) error {
	if err == nil {
		return nil
	}

	return &repositoryError{
		operation: operation, messageToken: privacylog.Pseudonym(messageID),
		reason: repositoryErrorReason(err), cause: err,
	}
}

func repositoryErrorReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	default:
		return "database_operation_failed"
	}
}
