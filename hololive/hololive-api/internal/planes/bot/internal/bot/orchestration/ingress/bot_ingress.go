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

package ingress

import (
	"context"
	"log/slog"
	"strings"

	"github.com/park285/iris-client-go/v2/webhook"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/acl"
)

// irisMessageTypeText: Iris 프로토콜에서 일반 텍스트 메시지를 나타내는 타입 값.
const irisMessageTypeText = "1"

type Envelope struct {
	CommandType string
	ChatID      string
	RoomName    string
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

// Accepts는 observation이나 command-received 부수효과 없이 메시지가 durable inbox에
// 들어갈 수 있는지 반환합니다. Prepare가 이 경로와 durable 처리에서 쓰는 ingress 조건을 소유합니다.
func (i *MessageIngress) Accepts(ctx context.Context, message *webhook.Message) bool {
	_, ok := i.prepare(ctx, message, false)

	return ok
}

func (i *MessageIngress) prepare(ctx context.Context, message *webhook.Message, observe bool) (*Envelope, bool) {
	if !i.canHandleMessage(ctx, message) {
		return nil, false
	}

	chatID, roomName := resolveRoom(message)
	userID, userName := resolveUser(message)
	roomAttr := roomLogAttr(chatID, roomName)

	if i.shouldSkipSender(ctx, message, roomAttr, userName) {
		return nil, false
	}

	if i.isRoomBlocked(ctx, roomName, chatID, roomAttr) {
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
		RoomName:    roomName,
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

	if message.JSON != nil && message.JSON.Type != "" && message.JSON.Type != irisMessageTypeText {
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

func (i *MessageIngress) isRoomBlocked(ctx context.Context, roomName, chatID string, roomAttr slog.Attr) bool {
	if i.acl == nil || i.acl.IsRoomAllowed(roomName, chatID) {
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

	if parsed.Type == domain.CommandUnknown {
		summaryAttrs := messageSummaryAttrs(message.Msg)
		attrs := make([]slog.Attr, 0, 1+len(summaryAttrs))

		attrs = append(attrs, roomAttr)
		attrs = append(attrs, summaryAttrs...)
		i.logDebug(ctx,
			"Unknown command ignored",
			attrs...,
		)

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
	if i.logger == nil || parsed == nil {
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
