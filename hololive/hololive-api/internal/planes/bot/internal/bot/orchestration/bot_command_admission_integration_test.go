package orchestration

import (
	"context"
	"strings"
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestBotProcessMessageRejectsUnknownIngressUserForExpensiveCommand(t *testing.T) {
	cacheClient := sharedtestutil.NewTestCacheService(t.Context(), t)
	executions := 0
	registry := handlers.NewRegistry()
	registry.Register(&testCommand{
		name: "broadcast_history",
		execute: func(context.Context, *botcommand.CommandContext, botcommand.Params) error {
			executions++
			return nil
		},
	})

	b := withMessagePath(&Bot{
		logger:         newBotTestLogger(),
		messageAdapter: messaging.NewMessageAdapter("!"),
		irisClient:     &testIrisClient{},
		formatter:      formatter.NewResponseFormatter("!", nil),
		transport:      newDispatchingTransport(&testIrisClient{}, formatter.NewResponseFormatter("!", nil)),
		cache:          cacheClient,
	}, registry)
	sender := testSenderName
	message := &webhook.Message{
		Msg:    "!방송이력",
		Room:   testRoomID,
		Sender: &sender,
		JSON:   &webhook.MessageJSON{MessageID: "m-1"},
	}

	err := b.ProcessMessage(t.Context(), message)
	if err == nil || !strings.Contains(err.Error(), "stable user and room identities are required") {
		t.Fatalf("ProcessMessage() with unknown ingress user error = %v", err)
	}

	if executions != 0 {
		t.Fatalf("handler executions with unknown ingress user = %d, want 0", executions)
	}

	message.JSON.UserID = testUserID
	if err := b.ProcessMessage(t.Context(), message); err != nil {
		t.Fatalf("ProcessMessage() with stable ingress user error = %v", err)
	}

	if executions != 1 {
		t.Fatalf("handler executions after stable ingress user = %d, want 1", executions)
	}
}
