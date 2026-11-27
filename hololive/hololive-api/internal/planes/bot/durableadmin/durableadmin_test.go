package durableadmin

import (
	"bytes"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability/durabilitytest"
)

const adminOperator = "operator@example.com"

func TestRunRejectsMalformedInvocations(t *testing.T) {
	for name, args := range map[string][]string{
		"no command":         nil,
		"unknown command":    {"replay"},
		"missing outbox id":  {"grant-replay", "--actor=" + adminOperator, "--reason=incident"},
		"positional args":    {"discard", "--outbox-id=1", "extra"},
		"import positionals": {"import-postgres", "now"},
		"list limit zero":    {"list-manual-review", "--limit=0"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, exitUsage, Run(t.Context(), args, io.Discard, io.Discard))
		})
	}

	require.Equal(t, exitOK, Run(t.Context(), []string{"grant-replay", "--help"}, io.Discard, io.Discard))
}

func TestRunAppliesOperatorDecisionsToTheAttachedStore(t *testing.T) {
	fixture := durabilitytest.Open(t)
	t.Setenv("BOT_DURABLE_STORE_PATH", fixture.Path)

	replayID := manualReviewReply(t, fixture, 0)
	discardID := manualReviewReply(t, fixture, 1)

	listed, code := runAdmin(t, "list-manual-review")
	require.Equal(t, exitOK, code)
	require.Contains(t, listed, "client-request-admin-0")
	require.Contains(t, listed, "client-request-admin-1")
	require.NotContains(t, listed, "answer", "the listing never prints reply payloads")

	stdout, code := runAdmin(t, "grant-replay", "--outbox-id="+itoa(replayID), "--actor="+adminOperator, "--reason=incident replay")
	require.Equal(t, exitOK, code)
	require.Equal(t, "outcome=replayed\n", stdout)

	stdout, code = runAdmin(t, "grant-replay", "--outbox-id="+itoa(replayID), "--actor="+adminOperator, "--reason=incident replay")
	require.Equal(t, exitFailed, code, "a rejected decision must fail the command")
	require.Equal(t, "outcome=not_manual_review\n", stdout)

	stdout, code = runAdmin(t, "discard", "--outbox-id="+itoa(discardID), "--actor="+adminOperator,
		"--reason=incident discard", "--observed-iris-state=not_found")
	require.Equal(t, exitOK, code)
	require.Equal(t, "outcome=discarded\n", stdout)

	var statuses []string

	rows, err := fixture.DB.QueryContext(t.Context(), "SELECT status FROM bot_reply_outbox ORDER BY id")
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	for rows.Next() {
		var status string

		require.NoError(t, rows.Scan(&status))

		statuses = append(statuses, status)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, []string{"pending", durability.ReplyOutboxDiscarded}, statuses)
}

func TestRunFailsWithoutAStorePath(t *testing.T) {
	t.Setenv("BOT_DURABLE_STORE_PATH", "")

	_, code := runAdmin(t, "grant-replay", "--outbox-id=1", "--actor="+adminOperator, "--reason=incident replay")
	require.Equal(t, exitFailed, code)
}

func runAdmin(t *testing.T, args ...string) (string, int) {
	t.Helper()

	var stdout bytes.Buffer

	code := Run(t.Context(), args, &stdout, io.Discard)

	return stdout.String(), code
}

func manualReviewReply(t *testing.T, fixture durabilitytest.Fixture, ordinal uint64) int64 {
	t.Helper()

	repository, err := durability.NewReplyOutboxRepositoryWithPolicy(fixture.Store, 5, time.Hour)
	require.NoError(t, err)

	clientRequestID := "client-request-admin-" + strconv.FormatUint(ordinal, 10)

	_, err = repository.Insert(t.Context(), &durability.ReplyOutboxEntry{
		MessageID: "message:admin", Phase: "reply", Ordinal: ordinal, RoomID: "room-1",
		Payload: []byte(`{"kind":"text","message":"answer"}`), ClientRequestID: clientRequestID,
	})
	require.NoError(t, err)

	var id int64

	require.NoError(t, fixture.DB.QueryRowContext(t.Context(), `UPDATE bot_reply_outbox
		SET row_version = row_version + 1, status = 'manual_review', attempts = 5, first_attempt_at = created_at
		WHERE client_request_id = ?1 RETURNING id`, clientRequestID).Scan(&id))

	return id
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}
