package botruntime

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/park285/shared-go/pkg/workercontract"
)

func workerAttemptOutcome(err error) workercontract.AttemptOutcome {
	if err == nil {
		return workercontract.AttemptSuccess
	}
	if orchestration.IsCommandOutcomeUnknown(err) {
		return workercontract.AttemptOutcomeUnknown
	}
	return failureAttemptOutcome(err)
}

func failureAttemptOutcome(err error) workercontract.AttemptOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return workercontract.AttemptTimeout
	}
	if errors.Is(err, context.Canceled) {
		return workercontract.AttemptCanceled
	}
	return workercontract.AttemptFailed
}

func commandExecutionStatus(commandErr error) string {
	if commandErr == nil {
		return durability.CommandExecutionSucceeded
	}
	if orchestration.IsCommandOutcomeUnknown(commandErr) || errors.Is(commandErr, context.Canceled) || errors.Is(commandErr, context.DeadlineExceeded) {
		return durability.CommandExecutionOutcomeUnknown
	}
	return durability.CommandExecutionFailed
}

func (r *durableRuntime) releaseInbox(ctx context.Context, claim *durability.InboxClaim, token string, cause error) {
	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, r.inboxMaxAttempts, r.inboxRetryAfter, inboxReleaseReason(cause))
	if err != nil {
		r.logError("release durable webhook", err)
		return
	}
	if outcome == durability.InboxReleaseAbandoned && r.logger != nil {
		r.logger.Error("durable webhook abandoned after max attempts",
			slog.Int("attempts", int(claim.Attempts)),
			slog.String("message_token", privacylog.Pseudonym(claim.MessageID)))
	}
}

func inboxReleaseReason(cause error) string {
	if cause == nil {
		return durability.InboxFailureCommandClaimFailed
	}
	switch {
	case errors.Is(cause, context.Canceled):
		return durability.InboxFailureCommandClaimContextCanceled
	case errors.Is(cause, context.DeadlineExceeded):
		return durability.InboxFailureCommandClaimContextDeadline
	default:
		return durability.InboxFailureCommandClaimFailed
	}
}
