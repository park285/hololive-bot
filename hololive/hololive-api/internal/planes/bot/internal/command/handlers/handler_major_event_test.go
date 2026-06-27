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

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
)

func TestMajorEventCommand_Name(t *testing.T) {
	cmd := &MajorEventCommand{}
	if cmd.Name() != "major_event" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "major_event")
	}
}

func TestMajorEventCommand_Description(t *testing.T) {
	cmd := &MajorEventCommand{}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

type stubMajorEventRepository struct {
	isSubscribed    bool
	isSubscribedErr error
	subscribeErr    error
	unsubscribeErr  error
}

func (s *stubMajorEventRepository) IsSubscribed(_ context.Context, _ string) (bool, error) {
	if s.isSubscribedErr != nil {
		return false, s.isSubscribedErr
	}

	return s.isSubscribed, nil
}

func (s *stubMajorEventRepository) Subscribe(_ context.Context, _, _ string) error {
	return s.subscribeErr
}

func (s *stubMajorEventRepository) Unsubscribe(_ context.Context, _ string) error {
	return s.unsubscribeErr
}

func newMajorEventErrorDeps(sentError *string) *Dependencies {
	return &Dependencies{
		Formatter:   adapter.NewResponseFormatter("!", nil),
		SendMessage: func(context.Context, string, string) error { return nil },
		SendError: func(_ context.Context, _, message string) error {
			*sentError = message
			return nil
		},
		Logger: slog.Default(),
	}
}

func TestMajorEventCommand_ServiceNotInitializedUsesSendError(t *testing.T) {
	var sentError string

	cmd := NewMajorEventCommand(newMajorEventErrorDeps(&sentError), nil)

	if err := cmd.ensureMajorEventReady(t.Context(), &domain.CommandContext{Room: "room-a"}); err != nil {
		t.Fatalf("ensureMajorEventReady returned error: %v", err)
	}

	if sentError != adapter.ErrMajorEventServiceNotInitialized {
		t.Fatalf("expected sendError %q, got %q", adapter.ErrMajorEventServiceNotInitialized, sentError)
	}
}

func TestMajorEventCommand_RepositoryErrorPathsUseSendError(t *testing.T) {
	cmdCtx := &domain.CommandContext{Room: "room-a", RoomName: "room-name"}

	tests := []struct {
		name       string
		repository MajorEventRepository
		params     map[string]any
		want       string
	}{
		{
			name:       "subscribe status check failure",
			repository: &stubMajorEventRepository{isSubscribedErr: errors.New("boom")},
			params:     map[string]any{"action": "on"},
			want:       adapter.ErrMajorEventStatusCheckFailed,
		},
		{
			name:       "subscribe failure",
			repository: &stubMajorEventRepository{isSubscribed: false, subscribeErr: errors.New("boom")},
			params:     map[string]any{"action": "on"},
			want:       adapter.ErrMajorEventSubscribeFailed,
		},
		{
			name:       "unsubscribe status check failure",
			repository: &stubMajorEventRepository{isSubscribedErr: errors.New("boom")},
			params:     map[string]any{"action": "off"},
			want:       adapter.ErrMajorEventStatusCheckFailed,
		},
		{
			name:       "unsubscribe failure",
			repository: &stubMajorEventRepository{isSubscribed: true, unsubscribeErr: errors.New("boom")},
			params:     map[string]any{"action": "off"},
			want:       adapter.ErrMajorEventUnsubscribeFailed,
		},
		{
			name:       "status status check failure",
			repository: &stubMajorEventRepository{isSubscribedErr: errors.New("boom")},
			params:     map[string]any{"action": "status"},
			want:       adapter.ErrMajorEventStatusCheckFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sentError string

			cmd := NewMajorEventCommand(newMajorEventErrorDeps(&sentError), tc.repository)

			if err := cmd.Execute(t.Context(), cmdCtx, tc.params); err != nil {
				t.Fatalf("execute returned error: %v", err)
			}

			if sentError != tc.want {
				t.Fatalf("expected sendError %q, got %q", tc.want, sentError)
			}
		})
	}
}
