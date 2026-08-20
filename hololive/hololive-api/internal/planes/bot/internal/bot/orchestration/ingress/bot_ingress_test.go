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
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/park285/shared-go/pkg/stringutil"
)

func TestMessageIngressPrepare_SkipsSelfSender(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	ingress := NewMessageIngress(
		messaging.NewMessageAdapter("!", ""),
		nil,
		logger,
		stringutil.Normalize("봇계정"),
	)

	sender := "봇계정"
	msg := &webhook.Message{
		Msg:    "!help",
		Room:   "테스트방",
		Sender: &sender,
	}

	envelope, ok := ingress.Prepare(t.Context(), msg)
	if ok {
		t.Fatal("expected self message to be skipped")
	}

	if envelope != nil {
		t.Fatal("expected nil envelope when skipped")
	}
}

func TestMessageIngressPrepare_ParsesCommand(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	ingress := NewMessageIngress(
		messaging.NewMessageAdapter("!", ""),
		nil,
		logger,
		"",
	)

	sender := "사용자"
	msg := &webhook.Message{
		Msg:    "!help",
		Room:   "홀로라이브",
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "chat-123",
		},
	}

	envelope, ok := ingress.Prepare(t.Context(), msg)
	if !ok {
		t.Fatal("expected command to be accepted")
	}

	if envelope == nil {
		t.Fatal("expected non-nil envelope")
	}

	if envelope.ChatID != "chat-123" {
		t.Fatalf("chat id = %q, want %q", envelope.ChatID, "chat-123")
	}

	if envelope.RoomName != "홀로라이브" {
		t.Fatalf("room name = %q, want %q", envelope.RoomName, "홀로라이브")
	}

	if envelope.UserID != "user-1" {
		t.Fatalf("user id = %q, want %q", envelope.UserID, "user-1")
	}

	if envelope.UserName != "사용자" {
		t.Fatalf("user name = %q, want %q", envelope.UserName, "사용자")
	}

	if envelope.CommandType != domain.CommandHelp.String() {
		t.Fatalf("command type = %q, want %q", envelope.CommandType, domain.CommandHelp.String())
	}
}

type recordingRooms struct {
	roomID, roomType, roomLinkID string
}

func (r *recordingRooms) Observe(_ context.Context, roomID, roomType, roomLinkID string) {
	r.roomID = roomID
	r.roomType = roomType
	r.roomLinkID = roomLinkID
}

func TestMessageIngressPrepare_ObservesRoomChat(t *testing.T) {
	t.Parallel()

	rooms := &recordingRooms{}
	ingress := NewMessageIngress(
		messaging.NewMessageAdapter("!", ""),
		nil,
		slog.New(slog.DiscardHandler),
		"",
		WithRoomObserver(rooms),
	)

	sender := "사용자"
	envelope, ok := ingress.Prepare(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   "room-title",
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID:     "user-1",
			ChatID:     "18446744073709551615",
			RoomType:   " MultiChat ",
			RoomLinkID: "",
		},
	})
	if !ok || envelope == nil {
		t.Fatal("expected command to be accepted")
	}
	if envelope.RoomType != "MultiChat" || envelope.RoomLinkID != "" {
		t.Fatalf("envelope room = %q/%q", envelope.RoomType, envelope.RoomLinkID)
	}
	if rooms.roomID != "18446744073709551615" || rooms.roomType != "MultiChat" {
		t.Fatalf("observed = %+v", rooms)
	}
}

func TestResolveRoom_NumericRoomPrefersRoomID(t *testing.T) {
	t.Parallel()

	message := &webhook.Message{
		Room: "123456",
		JSON: &webhook.MessageJSON{ChatID: "json-chat-id"},
	}

	chatID, roomName := resolveRoom(message)
	if chatID != "123456" {
		t.Fatalf("chat id = %q, want %q", chatID, "123456")
	}

	if roomName != "123456" {
		t.Fatalf("room name = %q, want %q", roomName, "123456")
	}
}
