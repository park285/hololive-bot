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

package news

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command/internal/handlers/internal/handlercore"
)

type stubMemberNewsService struct {
	digest             *membernewscontracts.Digest
	generateErr        error
	isSubscribed       bool
	isSubscribedErr    error
	subscribeErr       error
	unsubscribeErr     error
	subscribedRoomID   string
	subscribedRoomName string
	unsubscribedRoomID string
}

func (s *stubMemberNewsService) GenerateRoomDigest(_ context.Context, _ string, _ membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	if s.generateErr != nil {
		return nil, s.generateErr
	}

	return s.digest, nil
}

func (s *stubMemberNewsService) SubscribeRoom(_ context.Context, roomID, roomName string) error {
	if s.subscribeErr != nil {
		return s.subscribeErr
	}

	s.subscribedRoomID = roomID
	s.subscribedRoomName = roomName
	s.isSubscribed = true

	return nil
}

func (s *stubMemberNewsService) UnsubscribeRoom(_ context.Context, roomID string) error {
	if s.unsubscribeErr != nil {
		return s.unsubscribeErr
	}

	s.unsubscribedRoomID = roomID
	s.isSubscribed = false

	return nil
}

func (s *stubMemberNewsService) IsRoomSubscribed(_ context.Context, _ string) (bool, error) {
	if s.isSubscribedErr != nil {
		return false, s.isSubscribedErr
	}

	return s.isSubscribed, nil
}

func TestMemberNewsCommand_NoMembersMessage(t *testing.T) {
	formatter := adapter.NewResponseFormatter("!", nil)
	stub := &stubMemberNewsService{generateErr: membernewscontracts.ErrNoSubscribedMembers}

	var sentMessage string

	deps := &handlercore.Dependencies{
		MemberNews: stub,
		Formatter:  formatter,
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.Default(),
	}

	cmd := NewMemberNewsCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-a", RoomName: "room-name"}, map[string]any{"period": "weekly"})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	expected := formatter.FormatMemberNewsNoMembers(t.Context())
	if sentMessage != expected {
		t.Fatalf("expected %q, got %q", expected, sentMessage)
	}
}

func TestMemberNewsCommand_EnsureBaseDepsError(t *testing.T) {
	cmd := NewMemberNewsCommand(&handlercore.Dependencies{})

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-a"}, map[string]any{"period": "weekly"})
	if err == nil {
		t.Fatal("expected ensure base deps error, got nil")
	}

	if !strings.Contains(err.Error(), "ensure base deps") {
		t.Fatalf("expected wrapped ensure base deps error, got %v", err)
	}
}

func TestMemberNewsCommand_ServiceNotInitializedUsesSendError(t *testing.T) {
	formatter := adapter.NewResponseFormatter("!", nil)

	var sentError string

	deps := &handlercore.Dependencies{
		Formatter: formatter,
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.Default(),
	}

	cmd := NewMemberNewsCommand(deps)
	if err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-a", RoomName: "room-name"}, map[string]any{"period": "weekly"}); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if sentError != adapter.ErrMemberNewsServiceNotInitialized {
		t.Fatalf("expected sendError %q, got %q", adapter.ErrMemberNewsServiceNotInitialized, sentError)
	}
}

func TestMemberNewsCommand_ServiceErrorUsesSendError(t *testing.T) {
	formatter := adapter.NewResponseFormatter("!", nil)
	stub := &stubMemberNewsService{generateErr: errors.New("boom")}

	var sentError string

	deps := &handlercore.Dependencies{
		MemberNews: stub,
		Formatter:  formatter,
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.Default(),
	}

	cmd := NewMemberNewsCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-a", RoomName: "room-name"}, map[string]any{"period": "weekly"})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if sentError != adapter.ErrMemberNewsQueryFailed {
		t.Fatalf("expected sendError %q, got %q", adapter.ErrMemberNewsQueryFailed, sentError)
	}
}

func TestMemberNewsSubscriptionCommand_SubscribeAndStatus(t *testing.T) {
	formatter := adapter.NewResponseFormatter("!", nil)
	stub := &stubMemberNewsService{isSubscribed: false}

	var sentMessages []string

	deps := &handlercore.Dependencies{
		MemberNews: stub,
		Formatter:  formatter,
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessages = append(sentMessages, message)
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.Default(),
	}

	cmd := NewMemberNewsSubscriptionCommand(deps)
	ctx := &domain.CommandContext{Room: "room-a", RoomName: "room-name"}

	if err := cmd.Execute(t.Context(), ctx, map[string]any{"action": "on"}); err != nil {
		t.Fatalf("subscribe action returned error: %v", err)
	}

	if stub.subscribedRoomID != "room-a" || stub.subscribedRoomName != "room-name" {
		t.Fatalf("subscribe room args mismatch: id=%q name=%q", stub.subscribedRoomID, stub.subscribedRoomName)
	}

	if err := cmd.Execute(t.Context(), ctx, map[string]any{"action": "status"}); err != nil {
		t.Fatalf("status action returned error: %v", err)
	}

	if len(sentMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sentMessages))
	}

	if sentMessages[0] != formatter.FormatMemberNewsSubscribed(t.Context()) {
		t.Fatalf("unexpected subscribe message: %q", sentMessages[0])
	}

	if sentMessages[1] != formatter.FormatMemberNewsStatus(t.Context(), true) {
		t.Fatalf("unexpected status message: %q", sentMessages[1])
	}
}
