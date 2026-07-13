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
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/iris-client-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

type testCommand struct {
	name    string
	execute func(context.Context, *domain.CommandContext, map[string]any) error
}

func (c *testCommand) Name() string        { return c.name }
func (c *testCommand) Description() string { return "test" }
func (c *testCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if c.execute == nil {
		return nil
	}

	return c.execute(ctx, cmdCtx, params)
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
}

type sentMessage struct {
	room    string
	message string
}

type acceptedTestIrisClient struct {
	testIrisClient

	acceptedCalls int
	statuses      []*iris.ReplyStatusSnapshot
}

type acceptedImageTestIrisClient struct {
	acceptedTestIrisClient

	imageAcceptedCalls          int
	multipleImagesAcceptedCalls int
}

func (c *testIrisClient) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
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
		return nil, err
	}
	return &iris.ReplyAcceptedResponse{RequestID: "reply-test", Delivery: "queued", Room: room, Type: "text"}, nil
}

func (c *testIrisClient) SendImage(ctx context.Context, room string, imageData []byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()
	c.lastImageRoom = room
	c.lastImage = imageData
	c.mu.Unlock()

	return nil, c.sendImageErr
}

func (c *testIrisClient) SendMultipleImages(_ context.Context, room string, images [][]byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.mu.Lock()
	c.lastMultiImages = images
	c.mu.Unlock()

	return nil, c.sendMultipleImagesErr
}

func (c *testIrisClient) SendMarkdown(_ context.Context, _, _ string, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (c *testIrisClient) GetReplyStatus(_ context.Context, _ string) (*iris.ReplyStatusSnapshot, error) {
	return nil, nil
}

func (c *acceptedTestIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.acceptedCalls++
	if err := c.SendMessage(ctx, room, message, opts...); err != nil {
		return nil, err
	}
	return &iris.ReplyAcceptedResponse{RequestID: "reply-1", Delivery: "queued", Room: room, Type: "text"}, nil
}

func (c *acceptedTestIrisClient) GetReplyStatus(_ context.Context, _ string) (*iris.ReplyStatusSnapshot, error) {
	if len(c.statuses) == 0 {
		return &iris.ReplyStatusSnapshot{State: "handoff_completed"}, nil
	}
	status := c.statuses[0]
	c.statuses = c.statuses[1:]
	return status, nil
}

func (c *acceptedImageTestIrisClient) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.imageAcceptedCalls++
	if _, err := c.testIrisClient.SendImage(ctx, room, imageData, opts...); err != nil {
		return nil, err
	}
	return &iris.ReplyAcceptedResponse{RequestID: "reply-image-1", Delivery: "queued", Room: room, Type: "image"}, nil
}

func (c *acceptedImageTestIrisClient) SendMultipleImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.multipleImagesAcceptedCalls++
	if _, err := c.testIrisClient.SendMultipleImages(ctx, room, images, opts...); err != nil {
		return nil, err
	}
	return &iris.ReplyAcceptedResponse{RequestID: "reply-images-1", Delivery: "queued", Room: room, Type: "image_multiple"}, nil
}

func (c *testIrisClient) Ping(ctx context.Context) bool { return true }

func (c *testIrisClient) GetConfig(ctx context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}

func (c *testIrisClient) Decrypt(ctx context.Context, data string) (string, error) {
	return data, nil
}

func newBotTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestCommandRouterExecuteBranches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cmdCtx := domain.NewCommandContext("room-1", "room", "user-1", "user", "!help", false)

	t.Run("nil registry", func(t *testing.T) {
		router := orchcmd.NewCommandRouter(nil, newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)
		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command registry is not initialized")
	})

	t.Run("unknown command sends fallback", func(t *testing.T) {
		var gotRoom, gotMessage string

		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(_ context.Context, room, message string) error {
			gotRoom = room
			gotMessage = message

			return nil
		}, nil, nil)

		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.NoError(t, err)
		assert.Equal(t, "room-1", gotRoom)
		assert.Equal(t, messagestrings.FallbackSentinel, gotMessage)
	})

	t.Run("unknown command fallback send failure", func(t *testing.T) {
		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error {
			return errors.New("send failed")
		}, nil, nil)

		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send unknown command message")
	})

	t.Run("command execution failure", func(t *testing.T) {
		registry := command.NewRegistry()
		registry.Register(&testCommand{
			name: "help",
			execute: func(context.Context, *domain.CommandContext, map[string]any) error {
				return errors.New("handler failed")
			},
		})

		router := orchcmd.NewCommandRouter(registry, newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)

		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute command")
	})

	t.Run("normalize alarm add command", func(t *testing.T) {
		router := orchcmd.NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error { return nil }, nil, nil)
		key, params := router.NormalizeCommand(domain.CommandAlarmAdd, map[string]any{"member": "miko"})
		assert.Equal(t, orchcmd.CommandKeyAlarm, key)
		assert.Equal(t, "add", params["action"])
		assert.Equal(t, "miko", params["member"])
	})
}

func TestCommandTransportSendMethods(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("constructor", func(t *testing.T) {
		client := &testIrisClient{}
		transport := NewCommandTransport(client, nil)
		require.NotNil(t, transport)
	})

	t.Run("send message with nil client", func(t *testing.T) {
		var transport *CommandTransport

		err := transport.SendMessage(ctx, "room", "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("send message wraps iris error", func(t *testing.T) {
		client := &testIrisClient{sendMessageErr: errors.New("iris unavailable")}
		transport := NewCommandTransport(client, nil)

		err := transport.SendMessage(ctx, "room", "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message to room room")
	})

	t.Run("send message retries failed accepted reply once", func(t *testing.T) {
		failedDetail := "callback failed"
		client := &acceptedTestIrisClient{
			statuses: []*iris.ReplyStatusSnapshot{
				{State: "failed", Detail: &failedDetail},
				{State: "handoff_completed"},
			},
		}
		transport := NewCommandTransport(client, nil)

		err := transport.SendMessage(ctx, "room", "hello")
		require.NoError(t, err)
		assert.Equal(t, 2, client.acceptedCalls)
		assert.Equal(t, "hello", client.lastMessage)
	})

	t.Run("send image wraps iris error", func(t *testing.T) {
		client := &testIrisClient{sendImageErr: errors.New("image failed")}
		transport := NewCommandTransport(client, nil)

		err := transport.SendImage(ctx, "room", []byte("img"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image to room room")
	})

	t.Run("send image returns failed reply status", func(t *testing.T) {
		failedDetail := "image bridge send failed: image lease last modified mismatch"
		client := &acceptedImageTestIrisClient{
			acceptedTestIrisClient: acceptedTestIrisClient{
				statuses: []*iris.ReplyStatusSnapshot{{State: "failed", Detail: &failedDetail}},
			},
		}
		transport := NewCommandTransport(client, nil)

		err := transport.SendImage(ctx, "room", []byte("img"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image to room room")
		assert.Contains(t, err.Error(), "image lease last modified mismatch")
		assert.Equal(t, 1, client.imageAcceptedCalls)
	})

	t.Run("send image forwards byte data to client", func(t *testing.T) {
		client := &testIrisClient{}
		transport := NewCommandTransport(client, nil)

		imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG 매직 바이트
		err := transport.SendImage(ctx, "room-1", imageData)
		require.NoError(t, err)
		assert.Equal(t, "room-1", client.lastImageRoom)
		assert.Equal(t, imageData, client.lastImage)
	})

	t.Run("send image with nil client returns error", func(t *testing.T) {
		var transport *CommandTransport
		err := transport.SendImage(ctx, "room", []byte("data"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("send error resolves key via formatter", func(t *testing.T) {
		client := &testIrisClient{}
		store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler)) //nolint:contextcheck,nolintlint // dbtest 전용 pool 생성자라 t.Cleanup으로 자체 lifecycle을 관리하며 prod ctx 경로와 무관(호출처 69곳). 멀티모듈 게이트 스코프에서만 발화하는 call-graph 아티팩트라 발화가 환경 의존적이고, 미발화 환경의 nolintlint 미사용 오탐도 함께 억제한다.
		require.NoError(t, store.Load(ctx))
		formatter := adapter.NewResponseFormatter("!", nil, adapter.WithMessageStrings(store))
		transport := NewCommandTransport(client, formatter)

		require.NoError(t, transport.SendError(ctx, "room", adapter.ErrAlarmAddFailed))
		assert.Equal(t, "room", client.lastMessageRoom)
		want := store.GetContext(ctx, messagestrings.NamespaceError, "alarm_add_failed")
		require.NotEmpty(t, want)
		assert.Equal(t, want, client.lastMessage)
	})

	t.Run("send error fails closed to sentinel on unknown key", func(t *testing.T) {
		client := &testIrisClient{}
		formatter := adapter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(client, formatter)

		require.NoError(t, transport.SendError(ctx, "room", "totally_unknown_key"))
		assert.Equal(t, messagestrings.FallbackSentinel, client.lastMessage)
	})
}

func TestBotEnsureComponentsAndHandleMessage(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	msgCh := make(chan sentMessage, 1)
	irisClient := &testIrisClient{messageCh: msgCh}
	b := &Bot{
		logger:          logger,
		commandRegistry: command.NewRegistry(),
		messageAdapter:  adapter.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       adapter.NewResponseFormatter("!", nil),
	}

	commandExecutor := b.ensureCommandExecutor()
	require.NotNil(t, commandExecutor)
	assert.Same(t, commandExecutor, b.ensureCommandExecutor())

	ingress := b.ensureIngress()
	require.NotNil(t, ingress)
	assert.Same(t, ingress, b.ensureIngress())

	transport := b.ensureTransport()
	require.NotNil(t, transport)
	assert.Same(t, transport, b.ensureTransport())

	// 알 수 없는 command 경로: fallback 메시지가 전송돼야 한다
	sender := "user"
	b.HandleMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "room-1",
		},
	})

	select {
	case msg := <-msgCh:
		assert.Equal(t, "room-1", msg.room)
		assert.Equal(t, messagestrings.FallbackSentinel, msg.message)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive message in time")
	}
}

func TestBotHandleMessage_ErrorBranchAndErrorMessageMapping(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	msgCh := make(chan sentMessage, 1)
	irisClient := &testIrisClient{messageCh: msgCh}

	registry := command.NewRegistry()
	registry.Register(&testCommand{
		name: "help",
		execute: func(context.Context, *domain.CommandContext, map[string]any) error {
			return errors.New("boom")
		},
	})

	b := &Bot{
		logger:          logger,
		commandRegistry: registry,
		messageAdapter:  adapter.NewMessageAdapter("!", ""),
		irisClient:      irisClient,
		formatter:       adapter.NewResponseFormatter("!", nil),
	}

	sender := "user"
	b.HandleMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "room-1",
		},
	})

	select {
	case msg := <-msgCh:
		assert.Equal(t, "room-1", msg.room)
		assert.Equal(t, messagestrings.FallbackSentinel, msg.message)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive message in time")
	}

	t.Run("getErrorMessage mappings", func(t *testing.T) {
		assert.Empty(t, b.getErrorMessage(nil))

		irisServiceErr := appErrors.NewServiceError("msg", serviceNameIris, "send_message", errors.New("down"))
		assert.Equal(t, adapter.ErrIrisConnectionFailed, b.getErrorMessage(irisServiceErr))

		apiErr := appErrors.NewAPIError("api", 500, map[string]any{"operation": "fetch"})
		assert.Equal(t, adapter.ErrExternalAPICallFailed, b.getErrorMessage(apiErr))

		keyRotationErr := appErrors.NewKeyRotationError("key", 429, map[string]any{"url": "https://example.com"})
		assert.Equal(t, adapter.ErrExternalAPICallFailed, b.getErrorMessage(keyRotationErr))

		cacheErr := appErrors.NewCacheError("cache", "get", "k1", errors.New("down"))
		assert.Equal(t, adapter.ErrCacheConnectionFailed, b.getErrorMessage(cacheErr))

		validationErr := appErrors.NewValidationError("invalid input", "field", "v")
		assert.Equal(t, adapter.ErrCommandProcessingFailed, b.getErrorMessage(validationErr))

		assert.Equal(t, adapter.ErrCommandProcessingFailed, b.getErrorMessage(errors.New("generic error")))
	})
}
