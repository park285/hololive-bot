package orchestration

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
)

type repositoryReplyOutboxWriter struct {
	repo *durability.ReplyOutboxRepository
}

func (w repositoryReplyOutboxWriter) RecordReply(ctx context.Context, entry *transport.ReplyOutboxEntry) error {
	_, err := w.repo.Insert(ctx, &durability.ReplyOutboxEntry{
		MessageID: entry.MessageID, Phase: entry.Phase, Ordinal: entry.Ordinal,
		RoomID: entry.Room, Payload: []byte(entry.Payload), ClientRequestID: entry.ClientRequestID,
	})

	//nolint:wrapcheck // 테스트가 재현하는 redaction 체인을 그대로 대조하므로 이 어댑터는 계층을 덧붙이면 안 된다.
	return err
}

func TestCommandErrorResponseRepositoryFailureDoesNotLogReplyIdentity(t *testing.T) {
	const (
		rawID     = "message:raw-private-handler-id"
		causeText = "database rejected"
	)

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		CREATE FUNCTION fail_handler_reply_insert_for_privacy_test() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'database rejected % %', NEW.message_id, NEW.client_request_id; END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_handler_reply_insert_for_privacy_test
		BEFORE INSERT ON bot_reply_outbox
		FOR EACH ROW EXECUTE FUNCTION fail_handler_reply_insert_for_privacy_test()`)
	require.NoError(t, err)

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	writer := repositoryReplyOutboxWriter{repo: durability.NewReplyOutboxRepository(pool)}
	bot := &Bot{
		logger: logger,
		transport: transport.NewCommandTransport(&testIrisClient{}, nil,
			transport.WithReplyOutboxWriter(writer)),
	}
	ctx := transport.WithReplyIdentity(t.Context(), rawID)

	err = bot.handleCommandExecutionError(ctx, testRoomID, "privacy-test", errors.New("command failed"))
	require.Error(t, err)

	var pgErr *pgconn.PgError

	require.ErrorAs(t, err, &pgErr, "repository cause must remain available through staging wrappers")
	require.Contains(t, pgErr.Error(), rawID)
	require.NotContains(t, err.Error(), rawID)
	require.NotContains(t, err.Error(), causeText)
	require.Contains(t, err.Error(), "message_token=anon:")
	require.Contains(t, err.Error(), "reason=database_operation_failed")
	require.False(t, strings.Contains(logs.String(), rawID) || strings.Contains(logs.String(), causeText),
		"bot_message_handler -> sharedlog.ErrorAttrs exposed repository cause: %s", logs.String())
	require.Contains(t, logs.String(), `"error_message":"send error message: record reply: reply staging failed: insert reply outbox row: message_token=anon:`)
	require.Contains(t, logs.String(), `reason=database_operation_failed`)
}
