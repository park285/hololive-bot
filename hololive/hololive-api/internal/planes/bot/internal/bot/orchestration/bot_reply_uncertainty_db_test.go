package orchestration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/v3/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type lostReplyReceiptWriter struct {
	repositoryReplyOutboxWriter

	calls int
	cause error
}

func (w *lostReplyReceiptWriter) RecordReply(ctx context.Context, entry *transport.ReplyOutboxEntry) error {
	w.calls++
	if err := w.repositoryReplyOutboxWriter.RecordReply(ctx, entry); err != nil {
		return fmt.Errorf("store reply: %w", err)
	}

	if w.calls == 1 {
		return w.cause
	}

	return nil
}

func TestBotCommittedReplyWithLostReceiptDoesNotStageErrorResponse(t *testing.T) {
	pool := dbtest.NewPool(t)
	cause := errors.New("reply commit acknowledgement lost")
	writer := &lostReplyReceiptWriter{repo: durability.NewReplyOutboxRepository(pool), cause: cause}
	registry := command.NewRegistry()
	bot := &Bot{
		logger:          newBotTestLogger(),
		commandRegistry: registry,
		messageAdapter:  messaging.NewMessageAdapter("!", ""),
		irisClient:      &testIrisClient{},
		formatter:       messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(loadSeededBotMessageStrings(t))),
	}
	bot.SetReplyOutboxWriter(writer)
	registry.Register(&testCommand{
		name: testHelpCommandName,
		execute: func(ctx context.Context, commandContext *domain.CommandContext, _ map[string]any) error {
			return bot.sendMessage(ctx, commandContext.Room, "answer")
		},
	})

	err := bot.ProcessMessage(t.Context(), &webhook.Message{
		Msg: "!" + testHelpCommandName, Room: testRoomID,
		JSON: &webhook.MessageJSON{UserID: testUserID, MessageID: "lost-reply-receipt"},
	})
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, transport.ErrReplyStagingFailed)
	require.ErrorIs(t, err, ErrCommandOutcomeUnknown)
	require.Equal(t, 1, writer.calls, "an uncertain commit must not produce another response")

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM bot_reply_outbox").Scan(&count))
	require.Equal(t, 1, count)
}
