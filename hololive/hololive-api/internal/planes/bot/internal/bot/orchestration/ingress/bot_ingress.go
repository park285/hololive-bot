package ingress

import (
	"context"
	"log/slog"
	"strings"

	"github.com/park285/iris-client-go/v6/webhook"
	"github.com/park285/shared-go/core/stringutil"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/service/acl"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

type Envelope struct {
	CommandType string
	ChatID      string
	RoomType    string
	RoomLinkID  string
	UserID      string
	UserName    string
	Parsed      *messaging.ParsedCommand
}

type RoomObserver interface {
	Observe(ctx context.Context, roomID, roomType, roomLinkID string)
}

type MessageIngress struct {
	messageAdapter *messaging.MessageAdapter
	acl            *acl.Service
	logger         *slog.Logger
	selfSender     string
	rooms          RoomObserver
}

type IngressOption func(*MessageIngress)

func WithRoomObserver(rooms RoomObserver) IngressOption {
	return func(i *MessageIngress) {
		i.rooms = rooms
	}
}

func NewMessageIngress(
	messageAdapter *messaging.MessageAdapter,
	aclService *acl.Service,
	logger *slog.Logger,
	selfSender string,
	opts ...IngressOption,
) *MessageIngress {
	ingress := &MessageIngress{
		messageAdapter: messageAdapter,
		acl:            aclService,
		logger:         logger,
		selfSender:     selfSender,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(ingress)
		}
	}

	return ingress
}

func (i *MessageIngress) Prepare(ctx context.Context, message *webhook.Message) (*Envelope, bool) {
	return i.prepare(ctx, message, true)
}

func (i *MessageIngress) Accepts(ctx context.Context, message *webhook.Message) bool {
	_, ok := i.prepare(ctx, message, false)

	return ok
}

func (i *MessageIngress) prepare(ctx context.Context, message *webhook.Message, observe bool) (*Envelope, bool) {
	if !i.canHandleMessage(ctx, message) {
		return nil, false
	}

	chatID := message.Room
	userID, userName := resolveUser(message)
	roomAttr := sharedprivacylog.RoomIDAttr(chatID)

	if i.shouldSkipSender(ctx, message, roomAttr, userName) {
		return nil, false
	}

	if i.isRoomBlocked(ctx, chatID, roomAttr) {
		return nil, false
	}

	roomType, roomLinkID := roomChatFromMessage(message)

	if observe && i.rooms != nil {
		i.rooms.Observe(ctx, chatID, roomType, roomLinkID)
	}

	parsed := i.parseCommand(ctx, message, roomAttr)
	if parsed == nil {
		return nil, false
	}

	commandType := parsed.Type.String()

	if observe {
		i.logCommandReceived(ctx, parsed, commandType, userID, roomAttr)
	}

	return &Envelope{
		CommandType: commandType,
		ChatID:      chatID,
		RoomType:    roomType,
		RoomLinkID:  roomLinkID,
		UserID:      userID,
		UserName:    userName,
		Parsed:      parsed,
	}, true
}

func (i *MessageIngress) canHandleMessage(ctx context.Context, message *webhook.Message) bool {
	if message == nil {
		i.logWarn(ctx, "Nil message received")

		return false
	}

	if message.JSON != nil && message.JSON.Type != "" && message.JSON.Type != webhook.MessageTypeText {
		return false
	}

	if i.messageAdapter == nil {
		i.logWarn(ctx, "Message adapter is not configured")

		return false
	}

	return true
}

func (i *MessageIngress) shouldSkipSender(ctx context.Context, message *webhook.Message, roomAttr slog.Attr, userName string) bool {
	if !i.isSelfSender(userName) {
		return false
	}

	i.logDebug(ctx,
		"Skipping self-issued message",
		roomAttr,
		slog.Int("message_len", len(strings.TrimSpace(message.Msg))),
	)

	return true
}

func (i *MessageIngress) isRoomBlocked(ctx context.Context, chatID string, roomAttr slog.Attr) bool {
	if i.acl == nil || i.acl.IsRoomAllowed(chatID) {
		return false
	}

	i.logDebug(ctx,
		"Room blocked by ACL, ignoring message",
		roomAttr,
	)

	return true
}

func (i *MessageIngress) parseCommand(ctx context.Context, message *webhook.Message, roomAttr slog.Attr) *messaging.ParsedCommand {
	parsed := i.messageAdapter.ParseMessage(message)
	if parsed == nil {
		i.logWarn(ctx, "Parsed command is nil", roomAttr)

		return nil
	}

	if parsed.Type == botcommand.CommandUnknown {
		if i.debugEnabled(ctx) {
			summaryAttrs := messageSummaryAttrs(message.Msg)
			attrs := make([]slog.Attr, 0, 1+len(summaryAttrs))

			attrs = append(attrs, roomAttr)
			attrs = append(attrs, summaryAttrs...)
			i.logDebug(ctx,
				"Unknown command ignored",
				attrs...,
			)
		}

		return nil
	}

	return parsed
}

func (i *MessageIngress) logCommandReceived(
	ctx context.Context,
	parsed *messaging.ParsedCommand,
	commandType string,
	userID string,
	roomAttr slog.Attr,
) {
	if parsed == nil || !i.debugEnabled(ctx) {
		return
	}

	ctx = sharedlog.WithComponent(sharedlog.WithRuntime(ctx, "bot"), "ingress")
	sharedlog.Debug(
		ctx,
		i.logger,
		EventCommandReceived,
		"bot command received",
		ingressAttrs(commandType, userID, roomAttr, parsed.RawMessage)...,
	)
}

func (i *MessageIngress) isSelfSender(sender string) bool {
	canonical := stringutil.Normalize(sender)
	if canonical == "" || i.selfSender == "" {
		return false
	}

	return canonical == i.selfSender
}

func (i *MessageIngress) debugEnabled(ctx context.Context) bool {
	return i.logger != nil && i.logger.Enabled(ctx, slog.LevelDebug)
}

func (i *MessageIngress) logDebug(ctx context.Context, msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

func (i *MessageIngress) logWarn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
}
