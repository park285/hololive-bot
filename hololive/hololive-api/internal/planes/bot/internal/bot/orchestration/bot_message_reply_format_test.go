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

// handler가 요청 진입 때 고정한 방 유형 판정을 명령 ctx로 넘겨야 formatter의 템플릿 변형과 transport의 발송 경로가
// 같은 값을 쓴다. 판정이 빠지면 오픈채팅 응답도 plain 본문과 일반 텍스트 경로로 나간다.
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

			b := &Bot{
				logger:          newBotTestLogger(),
				commandRegistry: registry,
				messageAdapter:  messaging.NewMessageAdapter("!", ""),
				irisClient:      irisClient,
				transport:       transport,
			}

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
