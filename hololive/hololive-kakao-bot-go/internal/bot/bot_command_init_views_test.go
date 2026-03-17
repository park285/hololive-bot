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

package bot

import (
	"context"
	"log/slog"
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

type stubCommandInitMajorEventRepo struct{}

func (s *stubCommandInitMajorEventRepo) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	return false, nil
}

func (s *stubCommandInitMajorEventRepo) Subscribe(ctx context.Context, roomID, roomName string) error {
	return nil
}

func (s *stubCommandInitMajorEventRepo) Unsubscribe(ctx context.Context, roomID string) error {
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

func TestCommandInitView_DefensiveCopyFactories(t *testing.T) {
	factory := command.Factory(func(_ *command.Dependencies) command.Command { return nil })
	b := &Bot{
		commandFactories: []command.Factory{factory},
		logger:           slog.New(slog.DiscardHandler),
	}

	view := b.commandInitView()
	if len(view.commandFactories) != 1 {
		t.Fatalf("command factory count = %d, want 1", len(view.commandFactories))
	}

	b.commandFactories[0] = nil
	if view.commandFactories[0] == nil {
		t.Fatal("view command factories must be copied defensively")
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

func TestCommandInitView_BuildFactoriesCount(t *testing.T) {
	defaultCount := len(command.DefaultFactories())
	view := commandInitView{
		logger:           slog.New(slog.DiscardHandler),
		majorEventRepo:   &stubCommandInitMajorEventRepo{},
		memberNews:       &stubCommandInitMemberNewsService{},
		commandFactories: []command.Factory{func(_ *command.Dependencies) command.Command { return nil }},
	}

	factories := view.buildFactories()

	expected := defaultCount + 1 + len(command.MemberNewsFactories()) + 1
	if len(factories) != expected {
		t.Fatalf("factory count = %d, want %d", len(factories), expected)
	}
}

var (
	_ streamRuntime                = (*stubCommandInitStreamProvider)(nil)
	_ command.MajorEventRepository = (*stubCommandInitMajorEventRepo)(nil)
	_ command.MemberNewsService    = (*stubCommandInitMemberNewsService)(nil)
)
