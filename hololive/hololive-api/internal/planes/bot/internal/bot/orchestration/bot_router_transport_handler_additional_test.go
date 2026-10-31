// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/iris-client-go/v4/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appErrors "github.com/kapu/hololive-api/internal/planes/bot/internal/apperrors"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	bottransport "github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type testCommand struct {
	name    string
	execute func(context.Context, *botcommand.CommandContext, map[string]any) error
}

func (c *testCommand) Name() string        { return c.name }
func (c *testCommand) Description() string { return "test" }
func (c *testCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	if c.execute == nil {
		return nil
	}

	return c.execute(ctx, cmdCtx, params)
}

// dispatchingReplyOutbox는 transport가 기록한 응답을 outbox dispatcher와 같은 DispatchStoredReply로 바로 보낸다.
// 시험이 Iris client 관측으로 응답을 확인하면서도 production 발송 경로(원장 기록 → 저장 payload 발송)를 탄다.
type dispatchingReplyOutbox struct {
	client bottransport.ReplyClient
}

func (o dispatchingReplyOutbox) RecordReply(ctx context.Context, entry *bottransport.ReplyOutboxEntry) error {
	if _, err := bottransport.DispatchStoredReply(ctx, o.client, entry.Room, []byte(entry.Payload), entry.ClientRequestID); err != nil {
		return fmt.Errorf("dispatch stored reply: %w", err)
	}

	return nil
}

func newDispatchingTransport(client bottransport.ReplyClient, responseFormatter *messageformatter.ResponseFormatter, opts ...bottransport.Option) *bottransport.CommandTransport {
	return bottransport.NewCommandTransport(dispatchingReplyOutbox{client: client}, responseFormatter, opts...)
}

type testIrisClient struct {
	sendMessageErr        error
	sendImageErr          error
	sendMultipleImagesErr error

	mu sync.Mutex

	messageCh chan sentMessage

	lastMessageRoom string
	lastMessage     string
	lastImageRoom   string
	lastImage       []byte
	lastMultiImages [][]byte
	richTextCalls   int
}

type sentMessage struct {
	room    string
	message string
}

type acceptedTestIrisClient struct {
	testIrisClient

	acceptedCalls int
}

func (c *testIrisClient) SendMessage(_ context.Context, room, message string, _ ...iris.SendOption) error {
	c.mu.Lock()

	c.lastMessageRoom = room
	c.lastMessage = message

	ch := c.messageCh
	c.mu.Unlock()

	if ch != nil {
		select {
		case ch <- sentMessage{room: room, message: message}:
		default:
		}
	}

	return c.sendMessageErr
}

func (c *testIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	if err := c.SendMessage(ctx, room, message, opts...); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	return &iris.ReplyAcceptedResponse{RequestID: "reply-test", Delivery: testDeliveryQueued, Room: room, Type: "text"}, nil
}

func (c *testIrisClient) SendImage(_ context.Context, room string, imageData []byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()

	c.lastImageRoom = room
	c.lastImage = imageData
	c.mu.Unlock()

	if c.sendImageErr != nil {
		return nil, c.sendImageErr
	}

	return &iris.ReplyAcceptedResponse{RequestID: "reply-image-test", Delivery: testDeliveryQueued, Room: room, Type: "image"}, nil
}

func (c *testIrisClient) SendMultipleImages(_ context.Context, room string, images [][]byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()

	c.lastMultiImages = images
	c.mu.Unlock()

	if c.sendMultipleImagesErr != nil {
		return nil, c.sendMultipleImagesErr
	}

	return &iris.ReplyAcceptedResponse{RequestID: "reply-images-test", Delivery: testDeliveryQueued, Room: room, Type: "image_multiple"}, nil
}

func (c *testIrisClient) SendRichText(ctx context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()

	c.richTextCalls++
	c.mu.Unlock()

	if err := c.SendMessage(ctx, room, markdown, opts...); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	return &iris.ReplyAcceptedResponse{RequestID: "reply-richtext", Delivery: testDeliveryQueued, Room: room, Type: "richtext"}, nil
}

func (c *testIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("unexpected legacy markdown send")
}

func (c *testIrisClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	return &iris.ReplyStatusSnapshot{RequestID: requestID, State: "handoff_completed"}, nil
}

func (c *acceptedTestIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.acceptedCalls++
	if err := c.SendMessage(ctx, room, message, opts...); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	return &iris.ReplyAcceptedResponse{RequestID: "reply-1", Delivery: testDeliveryQueued, Room: room, Type: "text"}, nil
}

func (c *testIrisClient) Ping(context.Context) error { return nil }

func (c *testIrisClient) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	return &iris.RoomListResponse{}, nil
}

func (c *testIrisClient) Decrypt(_ context.Context, data string) (string, error) {
	return data, nil
}

// 시드 message_strings 문구. 코드 대체 문구가 없으므로 응답 문구는 DB 정본에서만 나온다.
const (
	seedUnknownCommandMessage    = "❌ 알 수 없는 명령입니다.\n!도움말에서 사용 가능한 명령을 확인할 수 있습니다."
	seedCommandProcessingMessage = "❌ 명령 처리 중 오류가 발생했습니다."
)

func loadSeededBotMessageStrings(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler)) //nolint:contextcheck,nolintlint // dbtest 전용 pool 생성자라 t.Cleanup으로 자체 lifecycle을 관리하며 prod ctx 경로와 무관하다.
	require.NoError(t, store.Load(t.Context()))

	return store
}

func newBotTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestCommandRouterExecuteBranches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cmdCtx := botcommand.NewCommandContext(testRoomID, "room", testUserID, testSenderName, "!help", false)

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()

		router := orchcmd.NewCommandRouter(nil, newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)
		err := router.Execute(ctx, cmdCtx, botcommand.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command registry is not initialized")
	})

	t.Run("command execution failure", func(t *testing.T) {
		t.Parallel()

		registry := command.NewRegistry()
		registry.Register(&testCommand{
			name: testHelpCommandName,
			execute: func(context.Context, *botcommand.CommandContext, map[string]any) error {
				return errors.New("handler failed")
			},
		})

		router := orchcmd.NewCommandRouter(registry, newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)

		err := router.Execute(ctx, cmdCtx, botcommand.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute command")
	})

	t.Run("normalize alarm add command", func(t *testing.T) {
		t.Parallel()

		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)
		key, params := router.NormalizeCommand(botcommand.CommandAlarmAdd, map[string]any{"member": "miko"})
		assert.Equal(t, orchcmd.CommandKeyAlarm, key)
		assert.Equal(t, "add", params["action"])
		assert.Equal(t, "miko", params["member"])
	})
}

// 알 수 없는 명령 응답은 message_strings(DB 정본)의 unknown_command 문구다. 코드 대체 문구가 없으므로
// 문구를 적재하지 않은 router는 빈 메시지를 보내지 않고 실패한다.
func TestCommandRouterUnknownCommandMessage(t *testing.T) {
	t.Parallel()

	cmdCtx := botcommand.NewCommandContext(testRoomID, "room", testUserID, testSenderName, "!help", false)

	t.Run("unknown command sends seeded message", func(t *testing.T) {
		t.Parallel()

		var gotRoom, gotMessage string

		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(_ context.Context, room, message string) error {
			gotRoom = room
			gotMessage = message

			return nil
		}, loadSeededBotMessageStrings(t), nil)

		err := router.Execute(t.Context(), cmdCtx, botcommand.CommandHelp, nil)
		require.NoError(t, err)
		assert.Equal(t, testRoomID, gotRoom)
		assert.Equal(t, seedUnknownCommandMessage, gotMessage)
	})

	t.Run("unknown command without loaded strings fails without sending", func(t *testing.T) {
		t.Parallel()

		sent := false
		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error {
			sent = true

			return nil
		}, nil, nil)

		err := router.Execute(t.Context(), cmdCtx, botcommand.CommandHelp, nil)
		require.Error(t, err)
		assert.False(t, sent, "empty unknown command message must not be sent")
	})

	t.Run("unknown command fallback send failure", func(t *testing.T) {
		t.Parallel()

		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error {
			return errors.New("send failed")
		}, loadSeededBotMessageStrings(t), nil)

		err := router.Execute(t.Context(), cmdCtx, botcommand.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send unknown command message")
	})
}

func TestCommandTransportSendMessageMethods(t *testing.T) {
	t.Parallel()

	ctx := bottransport.WithReplyIdentity(t.Context(), "message:transport-test")

	t.Run("constructor", func(t *testing.T) {
		t.Parallel()

		transport := newDispatchingTransport(&testIrisClient{}, nil)
		require.NotNil(t, transport)
	})

	t.Run("send message with nil client", func(t *testing.T) {
		t.Parallel()

		var transport *bottransport.CommandTransport

		err := transport.SendMessage(ctx, "room", "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transport is not configured")
	})

	t.Run("send message wraps iris error", func(t *testing.T) {
		t.Parallel()

		client := &testIrisClient{sendMessageErr: errors.New("iris unavailable")}
		transport := newDispatchingTransport(client, nil)

		err := transport.SendMessage(ctx, "room", "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
	})

	t.Run("markdown replies option routes through SendRichText", func(t *testing.T) {
		t.Parallel()

		client := &acceptedTestIrisClient{}
		transport := newDispatchingTransport(client, nil, bottransport.WithMarkdownReplies(true))

		require.NoError(t, transport.SendMessage(bottransport.WithRoomChat(ctx, "OM", ""), "room", "hello"))
		assert.Equal(t, 1, client.richTextCalls)
		assert.Equal(t, 0, client.acceptedCalls)
		assert.Equal(t, "hello", client.lastMessage)
	})
}

func TestCommandTransportSendImageMethods(t *testing.T) {
	t.Parallel()

	ctx := bottransport.WithReplyIdentity(t.Context(), "message:transport-test")

	t.Run("send image wraps iris error", func(t *testing.T) {
		t.Parallel()

		client := &testIrisClient{sendImageErr: errors.New("image failed")}
		transport := newDispatchingTransport(client, nil)

		err := transport.SendImage(ctx, "room", []byte("img"))
		require.ErrorIs(t, err, bottransport.ErrReplyStagingFailed)
		assert.Contains(t, err.Error(), "image failed")
	})

	t.Run("send image forwards byte data to client", func(t *testing.T) {
		t.Parallel()

		client := &testIrisClient{}
		transport := newDispatchingTransport(client, nil)

		imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG 매직 바이트
		err := transport.SendImage(ctx, testRoomID, imageData)
		require.NoError(t, err)
		assert.Equal(t, testRoomID, client.lastImageRoom)
		assert.Equal(t, imageData, client.lastImage)
	})

	t.Run("send image with nil client returns error", func(t *testing.T) {
		t.Parallel()

		var transport *bottransport.CommandTransport

		err := transport.SendImage(ctx, "room", []byte("data"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transport is not configured")
	})
}

func TestCommandTransportSendErrorMethods(t *testing.T) {
	t.Parallel()

	ctx := bottransport.WithReplyIdentity(t.Context(), "message:transport-test")

	t.Run("send error resolves key via formatter", func(t *testing.T) {
		t.Parallel()

		client := &testIrisClient{}
		store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler)) //nolint:contextcheck,nolintlint // dbtest 전용 pool 생성자라 t.Cleanup으로 자체 lifecycle을 관리하며 prod ctx 경로와 무관(호출처 69곳). 멀티모듈 게이트 스코프에서만 발화하는 call-graph 아티팩트라 발화가 환경 의존적이고, 미발화 환경의 nolintlint 미사용 오탐도 함께 억제한다.
		require.NoError(t, store.Load(ctx))

		formatter := messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(store))
		transport := newDispatchingTransport(client, formatter)

		require.NoError(t, transport.SendError(ctx, "room", messaging.ErrAlarmAddFailed))
		assert.Equal(t, "room", client.lastMessageRoom)

		want, _ := store.Lookup(messagestrings.NamespaceError, "alarm_add_failed")
		require.NotEmpty(t, want)
		assert.Equal(t, want, client.lastMessage)
	})

	t.Run("send error fails closed on unknown key without sending", func(t *testing.T) {
		t.Parallel()

		client := &testIrisClient{}
		formatter := messageformatter.NewResponseFormatter("!", nil)
		transport := newDispatchingTransport(client, formatter)

		require.Error(t, transport.SendError(ctx, "room", "totally_unknown_key"))
		assert.Empty(t, client.lastMessage)
	})
}

func TestBotEnsureComponentsAndProcessMessage(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	msgCh := make(chan sentMessage, 1)
	irisClient := &testIrisClient{messageCh: msgCh}
	messageStrings := loadSeededBotMessageStrings(t)
	b := &Bot{
		logger:          logger,
		commandRegistry: command.NewRegistry(),
		messageAdapter:  messaging.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(messageStrings)),
		transport:       newDispatchingTransport(irisClient, messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(messageStrings))),
		messageStrings:  messageStrings,
	}

	commandExecutor := b.ensureCommandExecutor()
	require.NotNil(t, commandExecutor)
	assert.Same(t, commandExecutor, b.ensureCommandExecutor())

	ingress := b.ensureIngress()
	require.NotNil(t, ingress)
	assert.Same(t, ingress, b.ensureIngress())

	// 알 수 없는 command 경로: message_strings의 unknown_command 문구가 전송돼야 한다
	sender := testSenderName
	require.NoError(t, b.ProcessMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   testRoomID,
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID:    testUserID,
			MessageID: "m-1",
		},
	}))

	select {
	case msg := <-msgCh:
		assert.Equal(t, testRoomID, msg.room)
		assert.Equal(t, seedUnknownCommandMessage, msg.message)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive message in time")
	}
}

func TestBotProcessMessage_ErrorBranchAndErrorMessageMapping(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	msgCh := make(chan sentMessage, 1)
	irisClient := &testIrisClient{messageCh: msgCh}

	registry := command.NewRegistry()
	registry.Register(&testCommand{
		name: testHelpCommandName,
		execute: func(context.Context, *botcommand.CommandContext, map[string]any) error {
			return errors.New("boom")
		},
	})

	b := &Bot{
		logger:          logger,
		commandRegistry: registry,
		messageAdapter:  messaging.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(loadSeededBotMessageStrings(t))),
		transport:       newDispatchingTransport(irisClient, messageformatter.NewResponseFormatter("!", nil, messageformatter.WithMessageStrings(loadSeededBotMessageStrings(t)))),
	}

	sender := testSenderName
	err := b.ProcessMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   testRoomID,
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID:    testUserID,
			MessageID: "m-1",
		},
	})
	require.EqualError(t, err, "execute command: command help: boom")

	select {
	case msg := <-msgCh:
		assert.Equal(t, testRoomID, msg.room)
		assert.Equal(t, seedCommandProcessingMessage, msg.message)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive message in time")
	}

	t.Run("getErrorMessage mappings", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, b.getErrorMessage(nil))

		irisServiceErr := appErrors.NewServiceError("msg", serviceNameIris, "send_message", errors.New("down"))
		assert.Equal(t, messaging.ErrIrisConnectionFailed, b.getErrorMessage(irisServiceErr))

		assert.Equal(t, messaging.ErrCommandProcessingFailed, b.getErrorMessage(errors.New("generic error")))
	})
}
