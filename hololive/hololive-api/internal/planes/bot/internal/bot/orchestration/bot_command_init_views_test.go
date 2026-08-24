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

package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubCommandInitStreamProvider struct{}

func (s *stubCommandInitStreamProvider) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) {
	return &domain.Channel{}, nil
}
func (s *stubCommandInitStreamProvider) Stop() {}

type stubCommandInitMajorEventRepository struct{}

func (s *stubCommandInitMajorEventRepository) IsSubscribed(context.Context, string) (bool, error) {
	return false, nil
}

func (s *stubCommandInitMajorEventRepository) Subscribe(context.Context, string, string) error {
	return nil
}

func (s *stubCommandInitMajorEventRepository) Unsubscribe(context.Context, string) error {
	return nil
}

type stubCommandInitMemberNewsService struct{}

func (s *stubCommandInitMemberNewsService) GenerateRoomDigest(context.Context, string, membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	return &membernewscontracts.Digest{}, nil
}

func (s *stubCommandInitMemberNewsService) SubscribeRoom(context.Context, string, string) error {
	return nil
}

func (s *stubCommandInitMemberNewsService) UnsubscribeRoom(context.Context, string) error {
	return nil
}

func (s *stubCommandInitMemberNewsService) IsRoomSubscribed(context.Context, string) (bool, error) {
	return false, nil
}

type stubCommandInitCommand struct {
	name     string
	exec     func(context.Context, *domain.CommandContext, map[string]any) error
	executed int
}

func (s *stubCommandInitCommand) Name() string {
	return s.name
}

func (s *stubCommandInitCommand) Description() string {
	return "stub"
}

func (s *stubCommandInitCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	s.executed++

	if s.exec != nil {
		if err := s.exec(ctx, cmdCtx, params); err != nil {
			return fmt.Errorf("exec: %w", err)
		}

		return nil
	}

	return nil
}

func TestCommandInitView_DefensiveCopyCommandBuilders(t *testing.T) {
	external := orchcmd.CommandBuilder(func(_ *handlercore.Dependencies) handlercore.Command {
		return &stubCommandInitCommand{name: testExternalCommandName}
	})
	b := &Bot{
		commandBuilders: []orchcmd.CommandBuilder{external},
		logger:          slog.New(slog.DiscardHandler),
	}

	view := b.commandInitView()
	if len(view.commandBuilders) != 1 {
		t.Fatalf("command builder count = %d, want 1", len(view.commandBuilders))
	}

	b.commandBuilders[0] = nil
	if view.commandBuilders[0] == nil {
		t.Fatal("view command builders must be copied defensively")
	}
}

func TestCommandInitView_ToCommandDependencies(t *testing.T) {
	streamProvider := &stubCommandInitStreamProvider{}
	b := &Bot{
		holodex:    streamProvider,
		memberNews: &stubCommandInitMemberNewsService{},
		logger:     slog.New(slog.DiscardHandler),
	}

	view := b.commandInitView()

	deps := view.toCommandDependencies(handlers.NewRegistry())
	if deps == nil {
		t.Fatal("toCommandDependencies() returned nil")
	}

	if deps.Holodex != streamProvider {
		t.Fatal("holodex mapping mismatch")
	}

	if deps.MemberNews == nil {
		t.Fatal("memberNews mapping mismatch")
	}

	if deps.Dispatcher == nil {
		t.Fatal("dispatcher must be initialized")
	}

	if deps.SendMessage == nil || deps.SendImage == nil || deps.SendImages == nil || deps.SendError == nil {
		t.Fatal("send function mappings must not be nil")
	}
}

func TestCommandInitView_AssemblesCommands(t *testing.T) {
	registry := handlers.NewRegistry()
	view := commandInitView{
		logger:               slog.New(slog.DiscardHandler),
		majorEventRepository: &stubCommandInitMajorEventRepository{},
		memberNews:           &stubCommandInitMemberNewsService{},
		commandBuilders: []orchcmd.CommandBuilder{
			nil,
			func(_ *handlercore.Dependencies) handlercore.Command {
				return &stubCommandInitCommand{name: testExternalCommandName}
			},
		},
	}

	deps := view.toCommandDependencies(registry)
	commands := view.buildCommands(deps)

	gotNames := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if cmd == nil {
			t.Fatal("buildCommands() returned nil command")
		}

		gotNames = append(gotNames, cmd.Name())
	}

	wantNames := []string{
		testHelpCommandName,
		"live",
		"upcoming",
		"schedule",
		"alarm",
		"member_info",
		"subscriber",
		"broadcast_history",
		"broadcast_thumbnail",
		"major_event",
		"member_news",
		"news_subscription",
		testExternalCommandName,
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("command names = %v, want %v", gotNames, wantNames)
	}
}

func TestCommandInitView_ExternalCommandBuilderUsesCurrentDependencies(t *testing.T) {
	registry := handlers.NewRegistry()
	targetName := domain.CommandType("external_target")
	target := &stubCommandInitCommand{name: string(targetName)}
	registry.Register(target)

	var builtDeps *handlercore.Dependencies

	builder := orchcmd.CommandBuilder(func(deps *handlercore.Dependencies) handlercore.Command {
		builtDeps = deps

		return &stubCommandInitCommand{
			name: testExternalCommandName,
			exec: func(ctx context.Context, cmdCtx *domain.CommandContext, _ map[string]any) error {
				if _, err := deps.Dispatcher.Publish(ctx, cmdCtx, handlercore.Event{Type: targetName}); err != nil {
					return fmt.Errorf("publish dispatcher event: %w", err)
				}

				return nil
			},
		}
	})

	view := commandInitView{
		logger:          slog.New(slog.DiscardHandler),
		commandBuilders: []orchcmd.CommandBuilder{builder},
	}

	deps := view.toCommandDependencies(registry)
	commands := view.buildCommands(deps)

	var external handlercore.Command

	for _, cmd := range commands {
		if cmd.Name() == testExternalCommandName {
			external = cmd
			break
		}
	}

	if external == nil {
		t.Fatal("external command was not assembled")
	}

	if builtDeps != deps {
		t.Fatal("external builder did not receive current command dependencies")
	}

	if builtDeps == nil {
		t.Fatal("external builder did not receive command dependencies")
	}

	if builtDeps.Dispatcher == nil {
		t.Fatal("external builder dispatcher was not initialized")
	}

	if err := external.Execute(t.Context(), &domain.CommandContext{}, nil); err != nil {
		t.Fatalf("external command execute failed: %v", err)
	}

	if target.executed != 1 {
		t.Fatalf("dispatcher target executed = %d, want 1", target.executed)
	}
}

var (
	_ streamRuntime                    = (*stubCommandInitStreamProvider)(nil)
	_ handlercore.MajorEventRepository = (*stubCommandInitMajorEventRepository)(nil)
	_ handlercore.MemberNewsService    = (*stubCommandInitMemberNewsService)(nil)
)
