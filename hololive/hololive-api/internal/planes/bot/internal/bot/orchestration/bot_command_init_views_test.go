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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	membernewsmodel "github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubCommandInitStreamProvider struct{}

func (s *stubCommandInitStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
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

type stubCommandInitCalendarFinder struct{}

func (s *stubCommandInitCalendarFinder) FindMembersWithCelebrationsInMonth(context.Context, int, int) ([]domain.CalendarEntry, error) {
	return nil, nil
}

type stubCommandInitMemberNewsService struct{}

func (s *stubCommandInitMemberNewsService) GenerateRoomDigest(context.Context, string, membernewsmodel.Period) (*membernewsmodel.Digest, error) {
	return &membernewsmodel.Digest{}, nil
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

func TestCommandInitView_ToCommandDependencies(t *testing.T) {
	streamProvider := &stubCommandInitStreamProvider{}
	b := &Bot{logger: slog.New(slog.DiscardHandler)}

	view := b.commandInitView(&Dependencies{
		Holodex:    streamProvider,
		MemberNews: &stubCommandInitMemberNewsService{},
	}, nil)

	deps := view.toCommandDependencies()
	if deps == nil {
		t.Fatal("toCommandDependencies() returned nil")
	}

	if deps.Holodex != streamProvider {
		t.Fatal("holodex mapping mismatch")
	}

	if deps.MemberNews == nil {
		t.Fatal("memberNews mapping mismatch")
	}

	if deps.SendMessage == nil || deps.SendImage == nil || deps.SendImages == nil || deps.SendError == nil {
		t.Fatal("send function mappings must not be nil")
	}
}

func TestCommandInitView_AssemblesCommands(t *testing.T) {
	b := &Bot{logger: slog.New(slog.DiscardHandler)}
	view := b.commandInitView(&Dependencies{
		MajorEventRepository: &stubCommandInitMajorEventRepository{},
		MemberNews:           &stubCommandInitMemberNewsService{},
	}, &stubCommandInitCalendarFinder{})

	deps := view.toCommandDependencies()
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
		"broadcast_history",
		"broadcast_thumbnail",
		"broadcast_time",
		"calendar",
		"major_event",
		"member_news",
		"news_subscription",
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("command names = %v, want %v", gotNames, wantNames)
	}
}

var (
	_ domain.StreamProvider                 = (*stubCommandInitStreamProvider)(nil)
	_ handlercore.CelebrationCalendarFinder = (*stubCommandInitCalendarFinder)(nil)
	_ handlercore.MajorEventRepository      = (*stubCommandInitMajorEventRepository)(nil)
	_ handlercore.MemberNewsService         = (*stubCommandInitMemberNewsService)(nil)
)
