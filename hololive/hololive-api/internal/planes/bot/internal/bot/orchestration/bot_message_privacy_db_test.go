package orchestration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/ncruces/go-sqlite3"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability/durabilitytest"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type repositoryReplyOutboxWriter struct {
	repo *durability.ReplyOutboxRepository
}

func (w repositoryReplyOutboxWriter) RecordReply(ctx context.Context, entry *transport.ReplyOutboxEntry) error {
	if _, err := w.repo.Insert(ctx, &durability.ReplyOutboxEntry{
		MessageID: entry.MessageID, Phase: entry.Phase, Ordinal: entry.Ordinal,
		RoomID: entry.Room, Payload: []byte(entry.Payload), ClientRequestID: entry.ClientRequestID,
	}); err != nil {
		return fmt.Errorf("persist reply: %w", err)
	}

	return nil
}

func TestCommandErrorResponseRepositoryFailureDoesNotLogReplyIdentity(t *testing.T) {
	const (
		rawID     = "message:raw-private-handler-id"
		causeText = "database rejected"
	)

	durable := durabilitytest.Open(t)
	_, err := durable.DB.ExecContext(t.Context(), `CREATE TRIGGER fail_handler_reply_insert_for_privacy_test
		BEFORE INSERT ON bot_reply_outbox
		BEGIN
			SELECT RAISE(ABORT, 'database rejected ' || NEW.message_id || ' ' || NEW.client_request_id);
		END`)
	require.NoError(t, err)

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	writer := repositoryReplyOutboxWriter{repo: newTestReplyOutboxRepository(t, durable.Store)}
	messageStrings := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	require.NoError(t, messageStrings.Load(t.Context()))

	bot := &Bot{
		logger:    logger,
		transport: transport.NewCommandTransport(writer, formatter.NewResponseFormatter("!", nil, formatter.WithMessageStrings(messageStrings))),
	}
	ctx := transport.WithReplyIdentity(t.Context(), rawID)

	err = bot.handleCommandExecutionError(ctx, testRoomID, "privacy-test", errors.New("command failed"))
	require.Error(t, err)

	sqliteErr, ok := errors.AsType[*sqlite3.Error](err)
	require.True(t, ok, "repository cause must remain available through staging wrappers: %T", err)
	require.Contains(t, sqliteErr.Error(), rawID)
	require.NotContains(t, err.Error(), rawID)
	require.NotContains(t, err.Error(), causeText)
	require.Contains(t, err.Error(), "message_token=anon:")
	require.Contains(t, err.Error(), "reason=database_operation_failed")
	require.False(t, strings.Contains(logs.String(), rawID) || strings.Contains(logs.String(), causeText),
		"bot_message_handler -> sharedlog.ErrorAttrs exposed repository cause: %s", logs.String())
	require.Contains(t, logs.String(), `"error_message":"send error reply: send error message: record reply: reply staging failed: persist reply: insert reply outbox row: message_token=anon:`)
	require.Contains(t, logs.String(), `reason=database_operation_failed`)
}

func newTestReplyOutboxRepository(t *testing.T, store *durability.Store) *durability.ReplyOutboxRepository {
	t.Helper()

	repository, err := durability.NewReplyOutboxRepositoryWithPolicy(store, 5, iris.ReplyAutomaticReplayHorizon)
	require.NoError(t, err)

	return repository
}
