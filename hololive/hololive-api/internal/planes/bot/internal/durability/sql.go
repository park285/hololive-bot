package durability

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = func() func(string) string {
	read := sqlassets.MustReader(sqlAssets, "queries")
	compose := strings.NewReplacer(
		"{{inbox_ready_relation}}", read("inbox_ready_relation.sql"),
		"{{reply_ready_relation}}", read("reply_ready_relation.sql"),
		"{{safety_expired}}", read("reply_safety_expired.sql"),
	)

	return func(name string) string {
		return compose.Replace(read(name))
	}
}()

var (
	commandExecutionClaimSQL        = mustSQL("command_execution_claim.sql")
	commandExecutionCompleteSQL     = mustSQL("command_execution_complete.sql")
	commandExecutionExpireStaleSQL  = mustSQL("command_execution_expire_stale.sql")
	commandExecutionHeartbeatSQL    = mustSQL("command_execution_heartbeat.sql")
	commandExecutionStateSQL        = mustSQL("command_execution_state.sql")
	durablePruneCommandSQL          = mustSQL("durable_prune_command.sql")
	durablePruneInboxSQL            = mustSQL("durable_prune_inbox.sql")
	durablePruneOutboxSQL           = mustSQL("durable_prune_outbox.sql")
	inboxAbandonSQL                 = mustSQL("inbox_abandon.sql")
	inboxAdmitSQL                   = mustSQL("inbox_admit.sql")
	inboxClaimSQL                   = mustSQL("inbox_claim.sql")
	inboxCompleteSQL                = mustSQL("inbox_complete.sql")
	inboxHeadAdvanceSQL             = mustSQL("inbox_head_advance.sql")
	inboxHeadRemoveSQL              = mustSQL("inbox_head_remove.sql")
	inboxHeadSuccessorSQL           = mustSQL("inbox_head_successor.sql")
	inboxHeartbeatSQL               = mustSQL("inbox_heartbeat.sql")
	inboxInstallHeadSQL             = mustSQL("inbox_install_head.sql")
	inboxReadySnapshotSQL           = mustSQL("inbox_ready_snapshot.sql")
	inboxReclaimCandidatesSQL       = mustSQL("inbox_reclaim_candidates.sql")
	inboxReclaimDeadSQL             = mustSQL("inbox_reclaim_dead.sql")
	inboxReclaimRetrySQL            = mustSQL("inbox_reclaim_retry.sql")
	inboxReleaseRetrySQL            = mustSQL("inbox_release_retry.sql")
	inboxReleaseSQL                 = mustSQL("inbox_release.sql")
	replyOutboxClaimCandidateSQL    = mustSQL("reply_outbox_claim_candidate.sql")
	replyOutboxClaimSafetyReviewSQL = mustSQL("reply_outbox_claim_safety_review.sql")
	replyOutboxClaimSQL             = mustSQL("reply_outbox_claim.sql")
	replyOutboxConflictSQL          = mustSQL("reply_outbox_conflict.sql")
	replyOutboxDiscardAuditSQL      = mustSQL("reply_outbox_discard_audit.sql")
	replyOutboxDiscardSQL           = mustSQL("reply_outbox_discard.sql")
	replyOutboxHandoffClaimSQL      = mustSQL("reply_outbox_handoff_claim.sql")
	replyOutboxHandoffRequeueSQL    = mustSQL("reply_outbox_handoff_requeue.sql")
	replyOutboxHandoffRescheduleSQL = mustSQL("reply_outbox_handoff_reschedule.sql")
	replyOutboxHandoffSettleSQL     = mustSQL("reply_outbox_handoff_settle.sql")
	replyOutboxHandoffTerminalSQL   = mustSQL("reply_outbox_handoff_terminal.sql")
	replyOutboxInsertSQL            = mustSQL("reply_outbox_insert.sql")
	replyOutboxManualReviewListSQL  = mustSQL("reply_outbox_manual_review_list.sql")
	replyOutboxManualReviewStatsSQL = mustSQL("reply_outbox_manual_review_stats.sql")
	replyOutboxOperatorTargetSQL    = mustSQL("reply_outbox_operator_target.sql")
	replyOutboxReadySnapshotSQL     = mustSQL("reply_outbox_ready_snapshot.sql")
	replyOutboxReclaimCandidatesSQL = mustSQL("reply_outbox_reclaim_candidates.sql")
	replyOutboxReclaimRequeueSQL    = mustSQL("reply_outbox_reclaim_requeue.sql")
	replyOutboxReclaimReviewSQL     = mustSQL("reply_outbox_reclaim_review.sql")
	replyOutboxReplayGrantAuditSQL  = mustSQL("reply_outbox_replay_grant_audit.sql")
	replyOutboxReplayGrantSQL       = mustSQL("reply_outbox_replay_grant.sql")
	replyOutboxSettleAcceptedSQL    = mustSQL("reply_outbox_settle_accepted.sql")
	replyOutboxSettleRetrySQL       = mustSQL("reply_outbox_settle_retry.sql")
	replyOutboxSettleSQL            = mustSQL("reply_outbox_settle.sql")
	replyOutboxSettleTerminalSQL    = mustSQL("reply_outbox_settle_terminal.sql")
	webhookNoncePruneSQL            = mustSQL("webhook_nonce_prune.sql")
	webhookNonceReserveSQL          = mustSQL("webhook_nonce_reserve.sql")
)

var (
	ErrStoreNotConfigured = errors.New("durability: store is not configured")
	ErrInvalidArgument    = errors.New("durability: invalid argument")
)

const (
	roomIDRuneLimit         = 256
	messageIDRuneLimit      = 512
	orderingKeyRuneLimit    = 512
	phaseRuneLimit          = 32
	claimTokenRuneLimit     = 256
	commandKindRuneLimit    = 128
	terminalReasonByteLimit = 512
	lastErrorByteLimit      = 8192
)

var clientRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,160}$`)

func ensureStore(store *Store) error {
	if store == nil {
		return ErrStoreNotConfigured
	}

	return nil
}

func requireIdentity(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.Join(ErrInvalidArgument, errors.New(name+" must not be empty"))
	}

	return trimmed, nil
}

func requireBoundedIdentity(name, value string, runeLimit int) (string, error) {
	trimmed, err := requireIdentity(name, value)
	if err != nil {
		return "", err
	}

	if utf8.RuneCountInString(trimmed) > runeLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("%s must be at most %d runes", name, runeLimit))
	}

	return trimmed, nil
}

func requireMessageIdentity(value string) (string, error) {
	identity := MessageIdentity(value)
	if identity == "" {
		return "", errors.Join(ErrInvalidArgument, errors.New("message id must not be empty"))
	}

	if utf8.RuneCountInString(identity) > messageIDRuneLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("message id must be at most %d runes", messageIDRuneLimit))
	}

	return identity, nil
}

func requireRoomID(value string) (string, error) {
	out, err := requireBoundedIdentity("room id", value, roomIDRuneLimit)
	if err != nil {
		return out, err
	}

	return out, nil
}

func requireBoundedCommandKind(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > commandKindRuneLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("command kind must be at most %d runes", commandKindRuneLimit))
	}

	return trimmed, nil
}

func requireClientRequestID(value string) (string, error) {
	trimmed, err := requireIdentity("client request id", value)
	if err != nil {
		return "", err
	}

	if !clientRequestIDPattern.MatchString(trimmed) {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("client request id %q must match %s", trimmed, clientRequestIDPattern))
	}

	return trimmed, nil
}

func leaseMilliseconds(lease time.Duration) (int64, error) {
	if lease < time.Millisecond {
		return 0, errors.Join(ErrInvalidArgument, errors.New("lease duration must be at least 1ms"))
	}

	return lease.Milliseconds(), nil
}

func clampColumnText(value string, limitBytes int) string {
	if limitBytes <= 0 || len(value) <= limitBytes {
		return value
	}

	suffix := fmt.Sprintf("...[truncated from %d bytes]", len(value))
	if len(suffix) >= limitBytes {
		return suffix[:limitBytes]
	}

	head := value[:limitBytes-len(suffix)]
	for head != "" {
		r, size := utf8.DecodeLastRuneInString(head)
		if r != utf8.RuneError || size != 1 {
			break
		}

		head = head[:len(head)-1]
	}

	return head + suffix
}
