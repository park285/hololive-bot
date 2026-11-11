package orchestration

import (
	"context"
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	bottransport "github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestProcessMessageFixesReplyFormatForCommand(t *testing.T) {
	t.Parallel()

	const body = "**안내** 본문"

	for roomType, want := range map[string]domain.TemplateFormat{
		"OM":        domain.TemplateFormatRichText,
		"MultiChat": domain.TemplateFormatPlain,
	} {
		t.Run(roomType, func(t *testing.T) {
			t.Parallel()

			irisClient := &testIrisClient{}
			transport := newDispatchingTransport(irisClient, nil, bottransport.WithMarkdownReplies(true))

			var got domain.TemplateFormat

			registry := command.NewRegistry()
			registry.Register(&testCommand{
				name: testHelpCommandName,
				execute: func(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
					got, _ = messageformatter.ReplyFormatFromContext(ctx)

					return transport.SendMessage(ctx, cmdCtx.Room, body)
				},
			})

			b := withMessagePath(&Bot{
				logger:         newBotTestLogger(),
				messageAdapter: messaging.NewMessageAdapter("!"),
				irisClient:     irisClient,
				transport:      transport,
			}, registry)

			sender := testSenderName
			require.NoError(t, b.ProcessMessage(t.Context(), &webhook.Message{
				Msg:    "!" + testHelpCommandName,
				Room:   testRoomID,
				Sender: &sender,
				JSON:   &webhook.MessageJSON{UserID: testUserID, MessageID: "m-1", RoomType: roomType},
			}))

			require.Equal(t, want, got)

			if want == domain.TemplateFormatRichText {
				require.Equal(t, 1, irisClient.richTextCalls)
				require.Equal(t, body, irisClient.lastMessage)

				return
			}

			require.Zero(t, irisClient.richTextCalls)
			require.Equal(t, kakaoformat.Render(body), irisClient.lastMessage)
		})
	}
}
