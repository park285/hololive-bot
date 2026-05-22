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
	"log/slog"
	"slices"
	"testing"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

type stubCommandInitStreamProvider struct{}

func (s *stubCommandInitStreamProvider) GetLiveStreams(ctx context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannel(ctx context.Context, channelID string) (*domain.Channel, error) {
	return nil, nil
}
func (s *stubCommandInitStreamProvider) Stop() {}

type stubCommandInitMajorEventRepository struct{}

func (s *stubCommandInitMajorEventRepository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	return false, nil
}

func (s *stubCommandInitMajorEventRepository) Subscribe(ctx context.Context, roomID, roomName string) error {
	return nil
}

func (s *stubCommandInitMajorEventRepository) Unsubscribe(ctx context.Context, roomID string) error {
	return nil
}

type stubCommandInitMemberNewsService struct{}

func (s *stubCommandInitMemberNewsService) GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error) {
	return nil, nil
}

func (s *stubCommandInitMemberNewsService) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	return nil
}

func (s *stubCommandInitMemberNewsService) UnsubscribeRoom(ctx context.Context, roomID string) error {
	return nil
}

func (s *stubCommandInitMemberNewsService) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
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
		return s.exec(ctx, cmdCtx, params)
	}

	return nil
}

func TestCommandInitView_DefensiveCopyCommandBuilders(t *testing.T) {
	external := CommandBuilder(func(_ *command.Dependencies) command.Command {
		return &stubCommandInitCommand{name: "external"}
	})
	b := &Bot{
		commandBuilders: []CommandBuilder{external},
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

	deps := view.toCommandDependencies(command.NewRegistry())
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

	if deps.SendMessage == nil || deps.SendImage == nil || deps.SendError == nil {
		t.Fatal("send function mappings must not be nil")
	}
}

func TestCommandInitView_AssemblesCommands(t *testing.T) {
	registry := command.NewRegistry()
	view := commandInitView{
		logger:         slog.New(slog.DiscardHandler),
		majorEventRepository: &stubCommandInitMajorEventRepository{},
		memberNews:     &stubCommandInitMemberNewsService{},
		commandBuilders: []CommandBuilder{
			nil,
			func(_ *command.Dependencies) command.Command {
				return &stubCommandInitCommand{name: "external"}
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
		"help",
		"live",
		"upcoming",
		"schedule",
		"alarm",
		"member_info",
		"subscriber",
		"stats",
		"major_event",
		"member_news",
		"news_subscription",
		"external",
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("command names = %v, want %v", gotNames, wantNames)
	}
}

func TestCommandInitView_ExternalCommandBuilderUsesCurrentDependencies(t *testing.T) {
	registry := command.NewRegistry()
	targetName := domain.CommandType("external_target")
	target := &stubCommandInitCommand{name: string(targetName)}
	registry.Register(target)

	var builtDeps *command.Dependencies
	builder := CommandBuilder(func(deps *command.Dependencies) command.Command {
		builtDeps = deps

		return &stubCommandInitCommand{
			name: "external",
			exec: func(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
				_, err := deps.Dispatcher.Publish(ctx, cmdCtx, command.Event{Type: targetName})
				return err
			},
		}
	})

	view := commandInitView{
		logger:          slog.New(slog.DiscardHandler),
		commandBuilders: []CommandBuilder{builder},
	}

	deps := view.toCommandDependencies(registry)
	commands := view.buildCommands(deps)

	var external command.Command
	for _, cmd := range commands {
		if cmd.Name() == "external" {
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
	_ streamRuntime                = (*stubCommandInitStreamProvider)(nil)
	_ command.MajorEventRepository = (*stubCommandInitMajorEventRepository)(nil)
	_ command.MemberNewsService    = (*stubCommandInitMemberNewsService)(nil)
)
