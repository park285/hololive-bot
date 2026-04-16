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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
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
	if message == nil {
		i.logWarn("Nil message received")
		return nil, false
	}

	if message.JSON != nil && message.JSON.Type != "" && message.JSON.Type != irisMessageTypeText {
		return nil, false
	}

	if i.messageAdapter == nil {
		i.logWarn("Message adapter is not configured")
		return nil, false
	}

	chatID, roomName := resolveRoom(message)
	userID, userName := resolveUser(message)

	if i.isSelfSender(userName) {
		i.logDebug(
			"Skipping self-issued message",
			slog.String("user", userName),
			slog.String("room", chatID),
			slog.String("payload", message.Msg),
		)

		return nil, false
	}

	if i.acl != nil && !i.acl.IsRoomAllowed(roomName, chatID) {
		i.logDebug(
			"Room blocked by ACL, ignoring message",
			slog.String("room", chatID),
			slog.String("room_name", roomName),
			slog.String("user_name", userName),
		)

		return nil, false
	}

	parsed := i.messageAdapter.ParseMessage(message)
	if parsed == nil {
		i.logWarn("Parsed command is nil", slog.String("room", chatID))
		return nil, false
	}

	commandType := parsed.Type.String()
	if parsed.Type == domain.CommandUnknown {
		i.logDebug(
			"Unknown command ignored",
			slog.String("msg", message.Msg),
			slog.String("room", chatID),
			slog.String("user_name", userName),
		)

		return nil, false
	}

	i.logInfo(
		"Command received",
		slog.String("raw", parsed.RawMessage),
		slog.String("type", commandType),
		slog.String("user_id", userID),
		slog.String("user_name", userName),
		slog.String("room", chatID),
		slog.String("room_name", roomName),
	)

	return &ingressEnvelope{
		CommandType: commandType,
		ChatID:      chatID,
		RoomName:    roomName,
		UserID:      userID,
		UserName:    userName,
		Parsed:      parsed,
	}, true
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

func (i *MessageIngress) logInfo(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(context.Background(), slog.LevelInfo, msg, attrs...)
}

func (i *MessageIngress) logWarn(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}

	i.logger.LogAttrs(context.Background(), slog.LevelWarn, msg, attrs...)
}
