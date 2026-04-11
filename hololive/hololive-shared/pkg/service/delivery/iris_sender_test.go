package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/iris-client-go/iris"
)

type stubIrisSender struct {
	err     error
	roomID  string
	message string
	called  int
}

func (s *stubIrisSender) SendMessage(_ context.Context, roomID, message string, _ ...iris.SendOption) error {
	s.called++
	s.roomID = roomID
	s.message = message
	return s.err
}

func TestNewIrisMessageSender_SendMessage(t *testing.T) {
	t.Parallel()

	stub := &stubIrisSender{}
	sender := NewIrisMessageSender(stub)
	if err := sender.SendMessage(context.Background(), "room-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if stub.called != 1 {
		t.Fatalf("called = %d, want 1", stub.called)
	}
	if stub.roomID != "room-1" {
		t.Fatalf("roomID = %q, want room-1", stub.roomID)
	}
	if stub.message != "hello" {
		t.Fatalf("message = %q, want hello", stub.message)
	}
}

func TestNewIrisMessageSender_SendMessageError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("send failed")
	sender := NewIrisMessageSender(&stubIrisSender{err: wantErr})
	if err := sender.SendMessage(context.Background(), "room-1", "hello"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewIrisMessageSender_NilClient(t *testing.T) {
	t.Parallel()

	sender := NewIrisMessageSender(nil)
	if err := sender.SendMessage(context.Background(), "room-1", "hello"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
