package mqutil

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseInboundMessage_Success: 정상적인 인바운드 메시지 파싱 테스트
func TestParseInboundMessage_Success(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]string
		want   InboundMessage
	}{
		{
			name: "Full fields (game-bot style: room, text, userId, sender, threadId)",
			fields: map[string]string{
				"room":     "chatroom123",
				"text":     "Hello World",
				"userId":   "user456",
				"sender":   "Alice",
				"threadId": "thread789",
			},
			want: InboundMessage{
				ChatID:   "chatroom123",
				UserID:   "user456",
				Content:  "Hello World",
				ThreadID: new("thread789"),
				Sender:   new("Alice"),
			},
		},
		{
			name: "content field (backwards compatible with assistant-bot)",
			fields: map[string]string{
				"room":    "chatroom123",
				"content": "Hi there",
				"userId":  "user456",
			},
			want: InboundMessage{
				ChatID:   "chatroom123",
				UserID:   "user456",
				Content:  "Hi there",
				ThreadID: nil,
				Sender:   nil,
			},
		},
		{
			name: "text field (game-bot)",
			fields: map[string]string{
				"room":   "chatroom123",
				"text":   "Game on",
				"userId": "user789",
			},
			want: InboundMessage{
				ChatID:   "chatroom123",
				UserID:   "user789",
				Content:  "Game on",
				ThreadID: nil,
				Sender:   nil,
			},
		},
		{
			name: "Minimal fields (no sender, no threadId)",
			fields: map[string]string{
				"room":   "chatroom123",
				"text":   "Message",
				"userId": "user456",
			},
			want: InboundMessage{
				ChatID:   "chatroom123",
				UserID:   "user456",
				Content:  "Message",
				ThreadID: nil,
				Sender:   nil,
			},
		},
		{
			name: "With leading/trailing whitespace",
			fields: map[string]string{
				"room":     "  chatroom123  ",
				"text":     "  Hello  ",
				"userId":   "  user456  ",
				"sender":   "  Alice  ",
				"threadId": "  thread789  ",
			},
			want: InboundMessage{
				ChatID:   "chatroom123",
				UserID:   "user456",
				Content:  "Hello",
				ThreadID: new("thread789"),
				Sender:   new("Alice"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseInboundMessage(tt.fields)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseInboundMessage_Errors: 인바운드 메시지 파싱 에러 케이스
func TestParseInboundMessage_Errors(t *testing.T) {
	tests := []struct {
		name    string
		fields  map[string]string
		wantErr error
	}{
		{
			name: "Missing chatID (room)",
			fields: map[string]string{
				"text":   "Hello",
				"userId": "user456",
			},
			wantErr: ErrMissingChatID,
		},
		{
			name: "Empty chatID after trim",
			fields: map[string]string{
				"room":   "   ",
				"text":   "Hello",
				"userId": "user456",
			},
			wantErr: ErrMissingChatID,
		},
		{
			name: "Missing content (text and content both empty)",
			fields: map[string]string{
				"room":   "chatroom123",
				"userId": "user456",
			},
			wantErr: ErrMissingContent,
		},
		{
			name: "Empty content after trim",
			fields: map[string]string{
				"room":   "chatroom123",
				"text":   "   ",
				"userId": "user456",
			},
			wantErr: ErrMissingContent,
		},
		{
			name: "Missing userID",
			fields: map[string]string{
				"room": "chatroom123",
				"text": "Hello",
			},
			wantErr: ErrMissingUserID,
		},
		{
			name: "Empty userID after trim",
			fields: map[string]string{
				"room":   "chatroom123",
				"text":   "Hello",
				"userId": "   ",
			},
			wantErr: ErrMissingUserID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseInboundMessage(tt.fields)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
		})
	}
}

// TestInboundMessage_String: String() 메서드 테스트
func TestInboundMessage_String(t *testing.T) {
	tests := []struct {
		name string
		msg  InboundMessage
		want string
	}{
		{
			name: "Full fields",
			msg: InboundMessage{
				ChatID:   "chat123",
				UserID:   "user456",
				Content:  "Hello",
				ThreadID: new("thread789"),
				Sender:   new("Alice"),
			},
			want: "chatId=chat123 userId=user456 threadId=thread789 sender=Alice",
		},
		{
			name: "Minimal fields",
			msg: InboundMessage{
				ChatID:  "chat123",
				UserID:  "user456",
				Content: "Hello",
			},
			want: "chatId=chat123 userId=user456 threadId= sender=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.msg.String())
		})
	}
}

// TestNewConstructors: OutboundMessage 생성자 테스트
func TestNewConstructors(t *testing.T) {
	chatID := "chat123"
	text := "Response message"
	threadID := new("thread789")

	tests := []struct {
		name     string
		fn       func(string, string, *string) OutboundMessage
		wantType OutboundType
	}{
		{"NewWaiting", NewWaiting, OutboundWaiting},
		{"NewFinal", NewFinal, OutboundFinal},
		{"NewError", NewError, OutboundError},
		{"NewReply", NewReply, OutboundReply},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.fn(chatID, text, threadID)
			assert.Equal(t, chatID, msg.ChatID)
			assert.Equal(t, text, msg.Text)
			assert.Equal(t, threadID, msg.ThreadID)
			assert.Equal(t, tt.wantType, msg.Type)
		})
	}
}

// TestOutboundMessage_ToStreamValues: ToStreamValues 메서드 테스트
func TestOutboundMessage_ToStreamValues(t *testing.T) {
	tests := []struct {
		name string
		msg  OutboundMessage
		want map[string]any
	}{
		{
			name: "With threadID",
			msg: OutboundMessage{
				ChatID:   "chat123",
				Text:     "Hello",
				ThreadID: new("thread789"),
				Type:     OutboundFinal,
			},
			want: map[string]any{
				"chatId":   "chat123",
				"text":     "Hello",
				"threadId": "thread789",
				"type":     "final",
			},
		},
		{
			name: "Without threadID",
			msg: OutboundMessage{
				ChatID: "chat123",
				Text:   "Hello",
				Type:   OutboundWaiting,
			},
			want: map[string]any{
				"chatId": "chat123",
				"text":   "Hello",
				"type":   "waiting",
			},
		},
		{
			name: "ThreadID with whitespace should be trimmed",
			msg: OutboundMessage{
				ChatID:   "chat123",
				Text:     "Hello",
				ThreadID: new("  thread789  "),
				Type:     OutboundError,
			},
			want: map[string]any{
				"chatId":   "chat123",
				"text":     "Hello",
				"threadId": "thread789",
				"type":     "error",
			},
		},
		{
			name: "ThreadID empty after trim should be excluded",
			msg: OutboundMessage{
				ChatID:   "chat123",
				Text:     "Hello",
				ThreadID: new("   "),
				Type:     OutboundReply,
			},
			want: map[string]any{
				"chatId": "chat123",
				"text":   "Hello",
				"type":   "reply",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.msg.ToStreamValues()
			assert.Equal(t, tt.want, got)
		})
	}
}

// Helper function: stringPtr
//
//go:fix inline
func stringPtr(s string) *string {
	return new(s)
}
