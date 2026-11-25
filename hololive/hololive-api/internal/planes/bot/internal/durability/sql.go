package durability

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = func() func(string) pgxdb.Statement {
	read := sqlassets.MustStatementReader(sqlAssets, "queries")
	inboxRelation := read("inbox_ready_relation.sql")
	replyRelation := read("reply_ready_relation.sql")
	safety := read("reply_safety_expired.sql")
	inboxClaim := read("inbox_claim.sql").With("{{inbox_ready_relation}}", inboxRelation)
	inboxSnapshot := read("inbox_ready_snapshot.sql").With("{{inbox_ready_relation}}", inboxRelation)
	inboxHeadAdvance := read("inbox_head_advance.sql")
	replyClaim := read("reply_outbox_claim.sql").
		With("{{reply_ready_relation}}", replyRelation).
		With("{{safety_expired}}", safety.WithToken("{{max_attempts}}", "$3").WithToken("{{replay_horizon}}", "$4"))
	replySnapshot := read("reply_outbox_ready_snapshot.sql").
		With("{{reply_ready_relation}}", replyRelation).
		With("{{safety_expired}}", safety.WithToken("{{max_attempts}}", "$1").WithToken("{{replay_horizon}}", "$2"))

	return func(name string) pgxdb.Statement {
		switch name {
		case "inbox_claim.sql":
			return inboxClaim
		case "inbox_ready_snapshot.sql":
			return inboxSnapshot
		case "inbox_abandon.sql", "inbox_complete.sql", "inbox_reclaim_expired.sql", "inbox_release.sql":
			return read(name).With("{{inbox_head_advance}}", inboxHeadAdvance)
		case "reply_outbox_claim.sql":
			return replyClaim
		case "reply_outbox_ready_snapshot.sql":
			return replySnapshot
		default:
			return read(name)
		}
	}
}()

var (
	commandExecutionClaimSQL        = mustSQL("command_execution_claim.sql")
	commandExecutionCompleteSQL     = mustSQL("command_execution_complete.sql")
	commandExecutionExpireStaleSQL  = mustSQL("command_execution_expire_stale.sql")
	commandExecutionHeartbeatSQL    = mustSQL("command_execution_heartbeat.sql")
	commandExecutionStateSQL        = mustSQL("command_execution_state.sql")
	durablePruneTerminalSQL         = mustSQL("durable_prune_terminal.sql")
	inboxAbandonSQL                 = mustSQL("inbox_abandon.sql")
	inboxAdmitSQL                   = mustSQL("inbox_admit.sql")
	inboxClaimSQL                   = mustSQL("inbox_claim.sql")
	inboxCompleteSQL                = mustSQL("inbox_complete.sql")
	inboxHeartbeatSQL               = mustSQL("inbox_heartbeat.sql")
	inboxMessageOrderingLockSQL     = mustSQL("inbox_message_ordering_lock.sql")
	inboxOrderingKeyLockSQL         = mustSQL("inbox_ordering_key_lock.sql")
	inboxOrderingKeysLockSQL        = mustSQL("inbox_ordering_keys_lock.sql")
	inboxReadySnapshotSQL           = mustSQL("inbox_ready_snapshot.sql")
	inboxReclaimCandidatesSQL       = mustSQL("inbox_reclaim_candidates.sql")
	inboxReclaimExpiredKeysSQL      = mustSQL("inbox_reclaim_expired_keys.sql")
	inboxReclaimExpiredSQL          = mustSQL("inbox_reclaim_expired.sql")
	inboxReleaseRetrySQL            = mustSQL("inbox_release_retry.sql")
	inboxReleaseSQL                 = mustSQL("inbox_release.sql")
	replyOutboxClaimSQL             = mustSQL("reply_outbox_claim.sql")
	replyOutboxConflictSQL          = mustSQL("reply_outbox_conflict.sql")
	replyOutboxHandoffClaimSQL      = mustSQL("reply_outbox_handoff_claim.sql")
	replyOutboxHandoffRequeueSQL    = mustSQL("reply_outbox_handoff_requeue.sql")
	replyOutboxHandoffRescheduleSQL = mustSQL("reply_outbox_handoff_reschedule.sql")
	replyOutboxHandoffSettleSQL     = mustSQL("reply_outbox_handoff_settle.sql")
	replyOutboxHandoffTerminalSQL   = mustSQL("reply_outbox_handoff_terminal.sql")
	replyOutboxInsertSQL            = mustSQL("reply_outbox_insert.sql")
	replyOutboxManualReviewStatsSQL = mustSQL("reply_outbox_manual_review_stats.sql")
	replyOutboxReadySnapshotSQL     = mustSQL("reply_outbox_ready_snapshot.sql")
	replyOutboxReclaimCandidatesSQL = mustSQL("reply_outbox_reclaim_candidates.sql")
	replyOutboxReclaimExpiredSQL    = mustSQL("reply_outbox_reclaim_expired.sql")
	replyOutboxSettleAcceptedSQL    = mustSQL("reply_outbox_settle_accepted.sql")
	replyOutboxSettleRetrySQL       = mustSQL("reply_outbox_settle_retry.sql")
	replyOutboxSettleSQL            = mustSQL("reply_outbox_settle.sql")
	replyOutboxSettleTerminalSQL    = mustSQL("reply_outbox_settle_terminal.sql")
)

var (
	ErrPoolNotConfigured = errors.New("durability: postgres pool is not configured")
	ErrInvalidArgument   = errors.New("durability: invalid argument")
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

const inboxStatusDead = "dead"

var clientRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,160}$`)

func ensurePool(pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrPoolNotConfigured
	}

	return nil
}

func jsonbParam(raw []byte) string {
	return string(raw)
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
