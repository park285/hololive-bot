package handlers

import (
	"context"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubCoverageStreamProvider struct{}

func (s *stubCoverageStreamProvider) GetUpcomingStreams(_ context.Context, _ int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCoverageStreamProvider) GetChannelSchedule(_ context.Context, _ string, _ int, _ bool) ([]*domain.Stream, error) {
	return nil, nil
}

func TestIntParam(t *testing.T) {
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
			key:          testParamLimit,
			defaultValue: 24,
			want:         24,
		},
		{
			name:         "nil map uses default",
			params:       nil,
			key:          testParamLimit,
			defaultValue: 12,
			want:         12,
		},
		{
			name:         "int value",
			params:       map[string]any{testParamLimit: 48},
			key:          testParamLimit,
			defaultValue: 24,
			want:         48,
		},
		{
			name:         "unsupported type uses default",
			params:       map[string]any{testParamLimit: "36"},
			key:          testParamLimit,
			defaultValue: 24,
			want:         24,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := intParam(tc.params, tc.key, tc.defaultValue)
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
		wantDisplayLimit int
	}{
		{
			name:             "defaults",
			params:           map[string]any{},
			wantDisplayLimit: 0,
		},
		{
			name:             "caps limit",
			params:           map[string]any{testParamLimit: 120},
			wantDisplayLimit: 100,
		},
		{
			name:             "show all overrides limit",
			params:           map[string]any{testParamLimit: 2, "all": true},
			wantDisplayLimit: 0,
		},
		{
			name:             "normalizes low limit",
			params:           map[string]any{testParamLimit: -1},
			wantDisplayLimit: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUpcomingOptions(tc.params)
			if got.displayLimit != tc.wantDisplayLimit {
				t.Fatalf("expected displayLimit %d, got %d", tc.wantDisplayLimit, got.displayLimit)
			}
		})
	}
}

func TestShouldSuppressSchedulePrompt(t *testing.T) {
	tests := []struct {
		name     string
		cmdCtx   *botcommand.CommandContext
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
			cmdCtx: &botcommand.CommandContext{Message: "!member"},
			want:   true,
		},
		{
			name:   "message with prefix and korean member",
			cmdCtx: &botcommand.CommandContext{Message: " / 멤버 "},
			want:   true,
		},
		{
			name:   "empty message",
			cmdCtx: &botcommand.CommandContext{Message: "   "},
			want:   false,
		},
		{
			name:   "non member message",
			cmdCtx: &botcommand.CommandContext{Message: "!일정"},
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
		deps    *handlercore.Dependencies
		wantErr string
	}{
		{
			name:    "nil deps",
			deps:    nil,
			wantErr: "deps is nil",
		},
		{
			name:    "nil matcher",
			deps:    &handlercore.Dependencies{},
			wantErr: "matcher is nil",
		},
		{
			name: "nil formatter",
			deps: &handlercore.Dependencies{
				Matcher: &matcher.Matcher{},
			},
			wantErr: "formatter is nil",
		},
		{
			name: "nil send error callback",
			deps: &handlercore.Dependencies{
				Matcher:   &matcher.Matcher{},
				Formatter: formatter.NewResponseFormatter("!", nil),
			},
			wantErr: "send error callback is nil",
		},
		{
			name: "all dependencies configured",
			deps: &handlercore.Dependencies{
				Matcher:   &matcher.Matcher{},
				Formatter: formatter.NewResponseFormatter("!", nil),
				SendError: func(_ context.Context, _, _ string) error {
					return nil
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := handlercore.ValidateMemberLookupDependencies(tc.deps)
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

func TestUpcomingCommandCheckDependencies(t *testing.T) {
	t.Run("base dependency error", func(t *testing.T) {
		cmd := NewUpcomingCommand(&handlercore.Dependencies{})

		err := cmd.CheckDependencies()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "ensure base dependencies: message callbacks not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("service dependency error", func(t *testing.T) {
		deps := &handlercore.Dependencies{
			SendMessage: func(_ context.Context, _, _ string) error { return nil },
			SendError:   func(_ context.Context, _, _ string) error { return nil },
		}
		cmd := NewUpcomingCommand(deps)

		err := cmd.CheckDependencies()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "upcoming command services not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		deps := &handlercore.Dependencies{
			CollabMembers: collabMembersStub{},
			Holodex:       &stubCoverageStreamProvider{},
			Matcher:       newUpcomingTestMatcher(nil),
			Formatter:     formatter.NewResponseFormatter("!", nil),
			SendMessage: func(_ context.Context, _, _ string) error {
				return nil
			},
			SendError: func(_ context.Context, _, _ string) error {
				return nil
			},
		}

		cmd := NewUpcomingCommand(deps)
		if err := cmd.CheckDependencies(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		if deps.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
	})
}

func TestScheduleCommandCheckDependencies(t *testing.T) {
	t.Run("base dependency error", func(t *testing.T) {
		cmd := NewScheduleCommand(&handlercore.Dependencies{})

		err := cmd.CheckDependencies()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "ensure base dependencies: message callbacks not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("service dependency error", func(t *testing.T) {
		deps := &handlercore.Dependencies{
			SendMessage: func(_ context.Context, _, _ string) error { return nil },
			SendError:   func(_ context.Context, _, _ string) error { return nil },
		}
		cmd := NewScheduleCommand(deps)

		err := cmd.CheckDependencies()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != "schedule command services not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		deps := &handlercore.Dependencies{
			CollabMembers: collabMembersStub{},
			Matcher:       &matcher.Matcher{},
			Holodex:       &stubCoverageStreamProvider{},
			Formatter:     formatter.NewResponseFormatter("!", nil),
			SendMessage: func(_ context.Context, _, _ string) error {
				return nil
			},
			SendError: func(_ context.Context, _, _ string) error {
				return nil
			},
		}

		cmd := NewScheduleCommand(deps)
		if err := cmd.CheckDependencies(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}

		if deps.Logger == nil {
			t.Fatal("expected logger to be initialized")
		}
	})
}
