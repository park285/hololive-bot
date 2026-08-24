package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
)

func (r *durableRuntime) claimCommand(ctx context.Context, claim *durability.InboxClaim, token string) (bool, error) {
	claimed, err := r.commands.Claim(ctx, claim.MessageID, "webhook", token)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)

		return false, fmt.Errorf("claim: %w", err)
	}

	if claimed {
		return true, nil
	}

	state, err := r.commands.State(ctx, claim.MessageID)
	if err != nil {
		r.releaseInbox(ctx, claim, token, err)

		return false, fmt.Errorf("state: %w", err)
	}

	if state == nil {
		err = errors.New("command execution state disappeared after claim conflict")
		r.releaseInbox(ctx, claim, token, err)

		return false, err
	}

	if state.Status == durability.CommandExecutionClaimed {
		if deferErr := r.deferInboxForClaimedCommand(ctx, claim, token, state.ClaimedAt); deferErr != nil {
			return false, fmt.Errorf("defer inbox for claimed command: %w", deferErr)
		}

		return false, nil
	}

	if _, err = r.inbox.Complete(ctx, claim.MessageID, token); err != nil {
		r.logError("complete already executed webhook", err)

		return false, fmt.Errorf("complete already executed webhook: %w", err)
	}

	return false, nil
}

func (r *durableRuntime) deferInboxForClaimedCommand(
	ctx context.Context,
	claim *durability.InboxClaim,
	token string,
	claimedAt time.Time,
) error {
	retryAfter := claimedAt.Add(commandStaleAfter + r.maintenanceEvery).Sub(r.nowTime())

	retryAfter = max(retryAfter, r.maintenanceEvery)

	outcome, err := r.inbox.Release(ctx, claim.MessageID, token, r.inboxMaxAttempts, retryAfter,
		durability.InboxFailureCommandAlreadyClaimed)
	if err != nil {
		r.logError("defer webhook behind active command claim", err)

		return fmt.Errorf("release: %w", err)
	}

	if outcome == durability.InboxReleaseAbandoned {
		err = errors.New("webhook exhausted retries behind active command claim")
		r.logError("defer webhook behind active command claim", err)

		return err
	}

	return nil
}

func (r *durableRuntime) completeCommandAndInbox(ctx context.Context, messageID, token string, commandErr error) error {
	status := commandExecutionStatus(commandErr)
	applied, completeErr := r.commands.Complete(ctx, messageID, token, status)

	if completeErr != nil || !applied {
		if completeErr == nil {
			completeErr = errors.New("command completion lost its claim")
		}

		r.logError("complete command execution", completeErr)

		return completeErr
	}

	if _, completeErr = r.inbox.Complete(ctx, messageID, token); completeErr != nil {
		r.logError("complete durable webhook", completeErr)

		return fmt.Errorf("complete: %w", completeErr)
	}

	if status == durability.CommandExecutionOutcomeUnknown {
		durableCommandOutcomeUnknownTotal.Inc()

		if r.logger != nil {
			r.logger.Error("durable command outcome requires manual review",
				slog.String("message_token", privacylog.Pseudonym(messageID)))
		}
	}

	return nil
}
