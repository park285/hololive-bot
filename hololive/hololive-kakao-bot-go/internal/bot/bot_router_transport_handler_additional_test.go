package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	appErrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
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
	sendMessageErr error
	sendImageErr   error

	lastMessageRoom string
	lastMessage     string
	lastImageRoom   string
	lastImage       string
}

func (c *testIrisClient) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	c.lastMessageRoom = room
	c.lastMessage = message
	return c.sendMessageErr
}

func (c *testIrisClient) SendImage(ctx context.Context, room, imageBase64 string) error {
	c.lastImageRoom = room
	c.lastImage = imageBase64
	return c.sendImageErr
}

func (c *testIrisClient) Ping(ctx context.Context) bool { return true }

func (c *testIrisClient) GetConfig(ctx context.Context) (*iris.Config, error) {
	return &iris.Config{}, nil
}

func (c *testIrisClient) Decrypt(ctx context.Context, data string) (string, error) {
	return data, nil
}

func newBotTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCommandRouterExecuteBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cmdCtx := domain.NewCommandContext("room-1", "room", "user-1", "user", "!help", false)

	t.Run("nil registry", func(t *testing.T) {
		router := NewCommandRouter(nil, newBotTestLogger(), func(context.Context, string, string) error { return nil })
		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command registry is not initialized")
	})

	t.Run("unknown command sends fallback", func(t *testing.T) {
		var gotRoom, gotMessage string
		router := NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(_ context.Context, room, message string) error {
			gotRoom = room
			gotMessage = message
			return nil
		})

		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.NoError(t, err)
		assert.Equal(t, "room-1", gotRoom)
		assert.Equal(t, adapter.ErrUnknownCommand, gotMessage)
	})

	t.Run("unknown command fallback send failure", func(t *testing.T) {
		router := NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error {
			return errors.New("send failed")
		})

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
		router := NewCommandRouter(registry, newBotTestLogger(), func(context.Context, string, string) error { return nil })

		err := router.Execute(ctx, cmdCtx, domain.CommandHelp, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute command")
	})

	t.Run("normalize alarm add command", func(t *testing.T) {
		router := NewCommandRouter(command.NewRegistry(), newBotTestLogger(), func(context.Context, string, string) error { return nil })
		key, params := router.normalizeCommand(domain.CommandAlarmAdd, map[string]any{"member": "miko"})
		assert.Equal(t, commandKeyAlarm, key)
		assert.Equal(t, "add", params["action"])
		assert.Equal(t, "miko", params["member"])
	})
}

func TestCommandTransportSendMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("constructor", func(t *testing.T) {
		client := &testIrisClient{}
		transport := NewCommandTransport(client, nil)
		require.NotNil(t, transport)
		assert.Equal(t, client, transport.irisClient)
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

	t.Run("send image wraps iris error", func(t *testing.T) {
		client := &testIrisClient{sendImageErr: errors.New("image failed")}
		transport := NewCommandTransport(client, nil)

		err := transport.SendImage(ctx, "room", "img")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image to room room")
	})

	t.Run("send error uses formatter", func(t *testing.T) {
		client := &testIrisClient{}
		formatter := adapter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(client, formatter)

		require.NoError(t, transport.SendError(ctx, "room", "boom"))
		assert.Equal(t, "room", client.lastMessageRoom)
		assert.True(t, strings.Contains(client.lastMessage, "boom"))
		assert.True(t, strings.Contains(client.lastMessage, "❌"))
	})
}

func TestBotEnsureComponentsAndHandleMessage(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	irisClient := &testIrisClient{}
	b := &Bot{
		logger:          logger,
		commandRegistry: command.NewRegistry(),
		messageAdapter:  adapter.NewMessageAdapter("!"),
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

	// unknown command path: fallback message should be sent
	sender := "user"
	b.HandleMessage(context.Background(), &iris.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &iris.MessageJSON{
			UserID: "user-1",
			ChatID: "room-1",
		},
	})
	assert.Equal(t, "room-1", irisClient.lastMessageRoom)
	assert.Equal(t, adapter.ErrUnknownCommand, irisClient.lastMessage)
}

func TestBotHandleMessage_ErrorBranchAndErrorMessageMapping(t *testing.T) {
	t.Parallel()

	logger := newBotTestLogger()
	irisClient := &testIrisClient{}

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
		messageAdapter:  adapter.NewMessageAdapter("!"),
		irisClient:      irisClient,
		formatter:       adapter.NewResponseFormatter("!", nil),
	}

	sender := "user"
	b.HandleMessage(context.Background(), &iris.Message{
		Msg:    "!help",
		Room:   "room-name",
		Sender: &sender,
		JSON: &iris.MessageJSON{
			UserID: "user-1",
			ChatID: "room-1",
		},
	})

	assert.Equal(t, "room-1", irisClient.lastMessageRoom)
	assert.Contains(t, irisClient.lastMessage, "help 명령어 처리 중 오류")

	t.Run("getErrorMessage mappings", func(t *testing.T) {
		assert.Equal(t, "", b.getErrorMessage(nil, "help"))

		aiErr := errors.New("외부 AI 서비스 장애: 일시적 오류")
		assert.Equal(t, aiErr.Error(), b.getErrorMessage(aiErr, "help"))

		irisServiceErr := appErrors.NewServiceError("msg", "iris", "send_message", errors.New("down"))
		assert.Equal(t, adapter.ErrIrisConnectionFailed, b.getErrorMessage(irisServiceErr, "help"))

		apiErr := appErrors.NewAPIError("api", 500, map[string]any{"operation": "fetch"})
		assert.Equal(t, adapter.ErrExternalAPICallFailed, b.getErrorMessage(apiErr, "help"))

		keyRotationErr := appErrors.NewKeyRotationError("key", 429, map[string]any{"url": "https://example.com"})
		assert.Equal(t, adapter.ErrExternalAPICallFailed, b.getErrorMessage(keyRotationErr, "help"))

		cacheErr := appErrors.NewCacheError("cache", "get", "k1", errors.New("down"))
		assert.Equal(t, adapter.ErrCacheConnectionFailed, b.getErrorMessage(cacheErr, "help"))

		validationErr := appErrors.NewValidationError("invalid input", "field", "v")
		assert.Equal(t, validationErr.Error(), b.getErrorMessage(validationErr, "help"))

		assert.Equal(t, adapter.ErrCacheConnectionFailed, b.getErrorMessage(errors.New("Valkey timeout"), "help"))

		fallback := b.getErrorMessage(errors.New("generic error"), "help")
		assert.Contains(t, fallback, "help 명령어 처리 중 오류")
	})
}
