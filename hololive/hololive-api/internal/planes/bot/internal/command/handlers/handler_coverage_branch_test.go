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
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
)

type stubCoverageStreamProvider struct{}

func (s *stubCoverageStreamProvider) GetLiveStreams(_ context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCoverageStreamProvider) GetUpcomingStreams(_ context.Context, _ int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCoverageStreamProvider) GetChannelSchedule(_ context.Context, _ string, _ int, _ bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCoverageStreamProvider) GetChannel(_ context.Context, _ string) (*domain.Channel, error) {
	return nil, nil
}

func TestParseUpcomingIntParam(t *testing.T) {
	tests := []struct {
		name         string
		params       map[string]any
		key          string
		defaultValue int
		want         int
	}{
		{
			name:         "missing key uses default",
			params:       map[string]any{},
			key:          "hours",
			defaultValue: 24,
			want:         24,
		},
		{
			name:         "nil map uses default",
			params:       nil,
			key:          "hours",
			defaultValue: 12,
			want:         12,
		},
		{
			name:         "int value",
			params:       map[string]any{"hours": 48},
			key:          "hours",
			defaultValue: 24,
			want:         48,
		},
		{
			name:         "float64 value",
			params:       map[string]any{"hours": 36.9},
			key:          "hours",
			defaultValue: 24,
			want:         36,
		},
		{
			name:         "unsupported type uses default",
			params:       map[string]any{"hours": "36"},
			key:          "hours",
			defaultValue: 24,
			want:         24,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUpcomingIntParam(tc.params, tc.key, tc.defaultValue)
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestNormalizeUpcomingHours(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  int
	}{
		{name: "less than one", hours: 0, want: 24},
		{name: "negative", hours: -5, want: 24},
		{name: "minimum boundary", hours: 1, want: 1},
		{name: "maximum boundary", hours: 168, want: 168},
		{name: "over maximum", hours: 999, want: 168},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeUpcomingHours(tc.hours)
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestNormalizeUpcomingDisplayLimit(t *testing.T) {
	tests := []struct {
		name         string
		displayLimit int
		showAll      bool
		want         int
	}{
		{name: "show all always zero", displayLimit: 5, showAll: true, want: 0},
		{name: "less than one means unlimited", displayLimit: 0, showAll: false, want: 0},
		{name: "over maximum", displayLimit: 150, showAll: false, want: 100},
		{name: "normal", displayLimit: 20, showAll: false, want: 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeUpcomingDisplayLimit(tc.displayLimit, tc.showAll)
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestParseUpcomingOptions(t *testing.T) {
	tests := []struct {
		name             string
		params           map[string]any
		wantHours        int
		wantDisplayLimit int
	}{
		{
			name:             "defaults",
			params:           map[string]any{},
			wantHours:        24,
			wantDisplayLimit: 0,
		},
		{
			name:             "caps hours and limit",
			params:           map[string]any{"hours": 200.0, "limit": 120.0},
			wantHours:        168,
			wantDisplayLimit: 100,
		},
		{
			name:             "show all overrides limit",
			params:           map[string]any{"hours": 0, "limit": 2, "all": true},
			wantHours:        24,
			wantDisplayLimit: 0,
		},
		{
			name:             "normalizes low limit",
			params:           map[string]any{"hours": 36, "limit": -1},
			wantHours:        36,
			wantDisplayLimit: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUpcomingOptions(tc.params)
			if got.hours != tc.wantHours {
				t.Fatalf("expected hours %d, got %d", tc.wantHours, got.hours)
			}

			if got.displayLimit != tc.wantDisplayLimit {
				t.Fatalf("expected displayLimit %d, got %d", tc.wantDisplayLimit, got.displayLimit)
			}
		})
	}
}

func TestShouldSuppressSchedulePrompt(t *testing.T) {
	tests := []struct {
		name     string
		cmdCtx   *domain.CommandContext
		rawToken string
		want     bool
	}{
		{
			name:     "raw token korean member",
			rawToken: "멤버",
			want:     true,
		},
		{
			name:     "raw token english member",
			rawToken: "member",
			want:     true,
		},
		{
			name:   "nil cmd context",
			cmdCtx: nil,
			want:   false,
		},
		{
			name:   "message with prefix and english member",
			cmdCtx: &domain.CommandContext{Message: "!member"},
			want:   true,
		},
		{
			name:   "message with prefix and korean member",
			cmdCtx: &domain.CommandContext{Message: " / 멤버 "},
			want:   true,
		},
		{
			name:   "empty message",
			cmdCtx: &domain.CommandContext{Message: "   "},
			want:   false,
		},
		{
			name:   "non member message",
			cmdCtx: &domain.CommandContext{Message: "!일정"},
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSuppressSchedulePrompt(tc.cmdCtx, tc.rawToken)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestValidateMemberLookupDependencies(t *testing.T) {
	tests := []struct {
		name    string
		deps    *Dependencies
		wantErr string
	}{
		{
			name:    "nil deps",
			deps:    nil,
			wantErr: "deps is nil",
		},
		{
			name:    "nil matcher",
			deps:    &Dependencies{},
			wantErr: "matcher is nil",
		},
		{
			name: "nil formatter",
			deps: &Dependencies{
				Matcher: &matcher.Matcher{},
			},
			wantErr: "formatter is nil",
		},
		{
			name: "nil send error callback",
			deps: &Dependencies{
				Matcher:   &matcher.Matcher{},
				Formatter: adapter.NewResponseFormatter("!", nil),
			},
			wantErr: "send error callback is nil",
		},
		{
			name: "all dependencies configured",
			deps: &Dependencies{
				Matcher:   &matcher.Matcher{},
				Formatter: adapter.NewResponseFormatter("!", nil),
				SendError: func(_ context.Context, _, _ string) error {
					return nil
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMemberLookupDependencies(tc.deps)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}

			if err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestUpcomingCommandEnsureDeps(t *testing.T) {
	t.Run("base dependency error", func(t *testing.T) {
		cmd := NewUpcomingCommand(&Dependencies{})

		err := cmd.ensureDeps()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "failed to ensure base dependencies: message callbacks not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("service dependency error", func(t *testing.T) {
		deps := &Dependencies{
			SendMessage: func(_ context.Context, _, _ string) error { return nil },
			SendError:   func(_ context.Context, _, _ string) error { return nil },
		}
		cmd := NewUpcomingCommand(deps)

		err := cmd.ensureDeps()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "upcoming command services not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		deps := &Dependencies{
			Holodex:   &stubCoverageStreamProvider{},
			Formatter: adapter.NewResponseFormatter("!", nil),
			SendMessage: func(_ context.Context, _, _ string) error {
				return nil
			},
			SendError: func(_ context.Context, _, _ string) error {
				return nil
			},
		}

		cmd := NewUpcomingCommand(deps)
		if err := cmd.ensureDeps(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		if deps.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
	})
}

func TestScheduleCommandEnsureDeps(t *testing.T) {
	t.Run("base dependency error", func(t *testing.T) {
		cmd := NewScheduleCommand(&Dependencies{})

		err := cmd.ensureDeps()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "failed to ensure base dependencies: message callbacks not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("service dependency error", func(t *testing.T) {
		deps := &Dependencies{
			SendMessage: func(_ context.Context, _, _ string) error { return nil },
			SendError:   func(_ context.Context, _, _ string) error { return nil },
		}
		cmd := NewScheduleCommand(deps)

		err := cmd.ensureDeps()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "schedule command services not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		deps := &Dependencies{
			Matcher:   &matcher.Matcher{},
			Holodex:   &stubCoverageStreamProvider{},
			Formatter: adapter.NewResponseFormatter("!", nil),
			SendMessage: func(_ context.Context, _, _ string) error {
				return nil
			},
			SendError: func(_ context.Context, _, _ string) error {
				return nil
			},
		}

		cmd := NewScheduleCommand(deps)
		if err := cmd.ensureDeps(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		if deps.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
	})
}
