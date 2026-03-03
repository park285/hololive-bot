package bot

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

type ingressEnvelope struct {
	CommandType string
	ChatID      string
	RoomName    string
	UserID      string
	UserName    string
	Parsed      *adapter.ParsedCommand
}

// MessageIngress: 수신 메시지의 필터링(자기 자신/ACL)과 파싱을 담당합니다.
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
			"Room not in ACL whitelist, ignoring message",
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
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	i.logger.Debug(msg, args...)
}

func (i *MessageIngress) logInfo(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	i.logger.Info(msg, args...)
}

func (i *MessageIngress) logWarn(msg string, attrs ...slog.Attr) {
	if i.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	i.logger.Warn(msg, args...)
}
