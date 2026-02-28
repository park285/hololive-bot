package mqutil

import (
	"errors"
	"fmt"
	"strings"
)

// MQ 메시지 파싱 에러 목록
var (
	// ErrMissingChatID: 채팅방 ID가 누락된 경우 반환되는 에러입니다.
	ErrMissingChatID = errors.New("missing chat_id")
	// ErrMissingContent: 메시지 내용이 누락된 경우 반환되는 에러입니다.
	ErrMissingContent = errors.New("missing content")
	// ErrMissingUserID: 사용자 ID가 누락된 경우 반환되는 에러입니다.
	ErrMissingUserID = errors.New("missing user_id")
)

// InboundMessage: MQ에서 수신한 인바운드 메시지 구조체입니다.
type InboundMessage struct {
	ChatID   string
	UserID   string
	Content  string // "content" 또는 "text" 필드를 매핑 (통일)
	ThreadID *string
	Sender   *string
}

// OutboundType: 아웃바운드 메시지의 유형을 나타냅니다.
type OutboundType string

// OutboundType 상수 목록
const (
	// OutboundWaiting: 처리 중임을 알리는 대기 메시지입니다.
	OutboundWaiting OutboundType = "waiting"
	// OutboundFinal: 최종 응답 메시지입니다.
	OutboundFinal OutboundType = "final"
	// OutboundError: 에러 응답 메시지입니다.
	OutboundError OutboundType = "error"
	// OutboundReply: 답장 메시지입니다 (assistant-bot 호환).
	OutboundReply OutboundType = "reply"
)

// OutboundMessage: MQ로 발행할 아웃바운드 메시지 구조체입니다.
type OutboundMessage struct {
	ChatID   string
	Text     string // Outbound는 Text 유지
	ThreadID *string
	Type     OutboundType
}

// ParseInboundMessage: Valkey stream 필드에서 인바운드 메시지를 파싱합니다.
// "content" 필드와 "text" 필드 모두 지원 (하위 호환성).
func ParseInboundMessage(fields map[string]string) (InboundMessage, error) {
	chatID := strings.TrimSpace(fields["room"])
	if chatID == "" {
		return InboundMessage{}, ErrMissingChatID
	}

	// "content" 필드 우선, 없으면 "text" 필드 사용 (하위 호환)
	content := strings.TrimSpace(fields["content"])
	if content == "" {
		content = strings.TrimSpace(fields["text"])
	}
	if content == "" {
		return InboundMessage{}, ErrMissingContent
	}

	userID := strings.TrimSpace(fields["userId"])
	if userID == "" {
		return InboundMessage{}, ErrMissingUserID
	}

	var threadIDPtr *string
	if threadID := strings.TrimSpace(fields["threadId"]); threadID != "" {
		threadIDPtr = &threadID
	}

	var senderPtr *string
	if sender := strings.TrimSpace(fields["sender"]); sender != "" {
		senderPtr = &sender
	}

	return InboundMessage{
		ChatID:   chatID,
		UserID:   userID,
		Content:  content,
		ThreadID: threadIDPtr,
		Sender:   senderPtr,
	}, nil
}

// NewWaiting: 처리 중 상태의 대기 메시지를 생성합니다.
func NewWaiting(chatID, text string, threadID *string) OutboundMessage {
	return OutboundMessage{ChatID: chatID, Text: text, ThreadID: threadID, Type: OutboundWaiting}
}

// NewFinal: 최종 응답 메시지를 생성합니다.
func NewFinal(chatID, text string, threadID *string) OutboundMessage {
	return OutboundMessage{ChatID: chatID, Text: text, ThreadID: threadID, Type: OutboundFinal}
}

// NewError: 에러 응답 메시지를 생성합니다.
func NewError(chatID, text string, threadID *string) OutboundMessage {
	return OutboundMessage{ChatID: chatID, Text: text, ThreadID: threadID, Type: OutboundError}
}

// NewReply: 답장 메시지를 생성합니다 (assistant-bot 호환).
func NewReply(chatID, text string, threadID *string) OutboundMessage {
	return OutboundMessage{ChatID: chatID, Text: text, ThreadID: threadID, Type: OutboundReply}
}

// ToStreamValues: 메시지를 Valkey Stream 발행용 map으로 변환합니다.
func (m OutboundMessage) ToStreamValues() map[string]any {
	values := map[string]any{
		"chatId": m.ChatID,
		"text":   m.Text,
		"type":   string(m.Type),
	}
	if m.ThreadID != nil {
		trimmed := strings.TrimSpace(*m.ThreadID)
		if trimmed != "" {
			values["threadId"] = trimmed
		}
	}
	return values
}

// String: 디버깅을 위한 문자열 표현을 반환합니다.
func (m InboundMessage) String() string {
	threadID := ""
	if m.ThreadID != nil {
		threadID = *m.ThreadID
	}
	sender := ""
	if m.Sender != nil {
		sender = *m.Sender
	}
	return fmt.Sprintf("chatId=%s userId=%s threadId=%s sender=%s", m.ChatID, m.UserID, threadID, sender)
}
