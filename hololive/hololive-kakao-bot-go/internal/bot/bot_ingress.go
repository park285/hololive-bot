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

package bot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-shared/pkg/service/acl"
)

// irisMessageTypeText: Iris 프로토콜에서 일반 텍스트 메시지를 나타내는 타입 값.
const irisMessageTypeText = "1"

type ingressEnvelope struct {
	CommandType string
	ChatID      string
	RoomName    string
	UserID      string
	UserName    string
	Parsed      *adapter.ParsedCommand
}

type MessageIngress struct {
	messageAdapter *adapter.MessageAdapter
	acl            *acl.Service
	logger         *slog.Logger
	selfSender     string
}

func NewMessageIngress(
	messageAdapter *adapter.MessageAdapter,
	aclSvc *acl.Service,
	logger *slog.Logger,
	selfSender string,
) *MessageIngress {
	return &MessageIngress{
		messageAdapter: messageAdapter,
		acl:            aclSvc,
		logger:         logger,
		selfSender:     selfSender,
	}
}

func (i *MessageIngress) Prepare(message *iris.Message) (*ingressEnvelope, bool) {
	if !i.canHandleMessage(message) {
		return nil, false
	}

	chatID, roomName := resolveRoom(message)
	userID, userName := resolveUser(message)

	if i.shouldSkipSender(message, chatID, userName) {
		return nil, false
	}

	if i.isRoomBlocked(roomName, chatID, userName) {
		return nil, false
	}

	parsed := i.parseCommand(message, chatID, userName)
	if parsed == nil {
		return nil, false
	}

	commandType := parsed.Type.String()
	i.logCommandReceived(parsed, commandType, userID, userName, chatID, roomName)

	return &ingressEnvelope{
		CommandType: commandType,
		ChatID:      chatID,
		RoomName:    roomName,
		UserID:      userID,
		UserName:    userName,
		Parsed:      parsed,
	}, true
}

func (i *MessageIngress) canHandleMessage(message *iris.Message) bool {
	if message == nil {
		i.logWarn("Nil message received")
		return false
	}

	if message.JSON != nil && message.JSON.Type != "" && message.JSON.Type != irisMessageTypeText {
		return false
	}

	if i.messageAdapter == nil {
		i.logWarn("Message adapter is not configured")
		return false
	}

	return true
}

func (i *MessageIngress) shouldSkipSender(message *iris.Message, chatID, userName string) bool {
	if !i.isSelfSender(userName) {
		return false
	}

	i.logDebug(
		"Skipping self-issued message",
		slog.String("user", userName),
		slog.String("room", chatID),
		slog.Int("message_len", len(strings.TrimSpace(message.Msg))),
	)

	return true
}

func (i *MessageIngress) isRoomBlocked(roomName, chatID, userName string) bool {
	if i.acl == nil || i.acl.IsRoomAllowed(roomName, chatID) {
		return false
	}

	i.logDebug(
		"Room blocked by ACL, ignoring message",
		slog.String("room", chatID),
		slog.String("room_name", roomName),
		slog.String("user_name", userName),
	)

	return true
}

func (i *MessageIngress) parseCommand(message *iris.Message, chatID, userName string) *adapter.ParsedCommand {
	parsed := i.messageAdapter.ParseMessage(message)
	if parsed == nil {
		i.logWarn("Parsed command is nil", slog.String("room", chatID))
		return nil
	}

	if parsed.Type == domain.CommandUnknown {
		attrs := []slog.Attr{
			slog.String("room", chatID),
			slog.String("user_name", userName),
		}
		attrs = append(attrs, messageSummaryAttrs(message.Msg)...)
		i.logDebug(
			"Unknown command ignored",
			attrs...,
		)

		return nil
	}

	return parsed
}

func (i *MessageIngress) logCommandReceived(
	parsed *adapter.ParsedCommand,
	commandType string,
	userID string,
	userName string,
	chatID string,
	roomName string,
) {
	if i.logger == nil || parsed == nil {
		return
	}
	ctx := sharedlog.WithComponent(sharedlog.WithRuntime(context.Background(), "bot"), "ingress")
	sharedlog.Info(
		ctx,
		i.logger,
		EventBotCommandReceived,
		"bot command received",
		ingressAttrs(commandType, userID, userName, chatID, roomName, parsed.RawMessage)...,
	)
}

func (i *MessageIngress) isSelfSender(sender string) bool {
	canonical := stringutil.Normalize(sender)
	if canonical == "" || i.selfSender == "" {
		return false
	}

	return canonical == i.selfSender
}

func (i *MessageIngress) logDebug(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
}

func (i *MessageIngress) logWarn(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(context.Background(), slog.LevelWarn, msg, attrs...)
}
