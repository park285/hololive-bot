package handlers

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type schedulePosterStub struct {
	schedule  scheduleposter.PublishedSchedule
	found     bool
	err       error
	channelID string
	day       time.Time
}

func (s *schedulePosterStub) PublishedRows(_ context.Context, channelID string, day time.Time) (scheduleposter.PublishedSchedule, bool, error) {
	s.channelID, s.day = channelID, day

	return s.schedule, s.found, s.err
}

type scheduleCardStub struct {
	data     []byte
	err      error
	member   *domain.Member
	schedule scheduleposter.PublishedSchedule
	calls    int
}

func (s *scheduleCardStub) RenderScheduleCard(_ context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule) ([]byte, error) {
	s.calls++

	s.member, s.schedule = member, schedule

	return s.data, s.err
}

type posterReplies struct {
	messages     []string
	errors       []string
	images       [][]byte
	contentTypes []string
}

func schedulePosterDeps(t *testing.T, posters handlercore.SchedulePosterReader, cards handlercore.ScheduleCardRenderer, replies *posterReplies, imageErr error) *handlercore.Dependencies {
	t.Helper()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelMiko, Name: "Miko", NameKo: "사쿠라 미코", ShortKoreanName: "미코"}})

	return &handlercore.Dependencies{
		Matcher:         matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:       formatter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SchedulePosters: posters,
		ScheduleCards:   cards,
		SendMessage: func(_ context.Context, _, message string) error {
			replies.messages = append(replies.messages, message)

			return nil
		},
		SendImage: func(ctx context.Context, _ string, image []byte) error {
			contentType, _ := transport.ImageContentTypeFromContext(ctx)

			replies.images = append(replies.images, image)
			replies.contentTypes = append(replies.contentTypes, contentType)

			return imageErr
		},
		SendError: func(_ context.Context, _, key string) error {
			replies.errors = append(replies.errors, key)

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}
}

func executeSchedulePoster(t *testing.T, deps *handlercore.Dependencies, params map[string]any) error {
	t.Helper()

	return NewSchedulePosterCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, params)
}

func publishedMikoSchedule() scheduleposter.PublishedSchedule {
	return scheduleposter.PublishedSchedule{
		PosterID:    7,
		WeekStart:   time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC),
		WeekEnd:     time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC),
		ExtractedAt: time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC),
		Rows:        []scheduleposter.Row{{Ordinal: 1, Day: time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC), Status: scheduleposter.RowPlanned, TitleKo: "잡담"}},
	}
}

func TestSchedulePosterCommandSendsRenderedCardAsPNG(t *testing.T) {
	posters := &schedulePosterStub{schedule: publishedMikoSchedule(), found: true}
	cards := &scheduleCardStub{data: []byte("card")}
	replies := &posterReplies{}

	cmd := NewSchedulePosterCommand(schedulePosterDeps(t, posters, cards, replies, nil))

	cmd.now = func() time.Time { return time.Date(2026, time.November, 10, 16, 30, 0, 0, time.UTC) }

	if err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{paramMember: "미코"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.messages) != 0 || len(replies.images) != 1 || string(replies.images[0]) != "card" || replies.contentTypes[0] != "image/png" {
		t.Fatalf("replies = %+v, want the rendered PNG card only", replies)
	}

	if posters.channelID != testChannelMiko || !posters.day.Equal(time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rows lookup = %s %s, want JST day 2026-11-11", posters.channelID, posters.day)
	}

	if cards.member == nil || cards.member.ChannelID != testChannelMiko || cards.schedule.PosterID != 7 {
		t.Fatalf("render input = %+v %+v, want miko and poster 7", cards.member, cards.schedule)
	}
}

func TestSchedulePosterCommandReportsInputAndLookupErrorsWithSeededText(t *testing.T) {
	seeded := newSeededTestFormatter(t)

	for _, tc := range []struct {
		name    string
		params  map[string]any
		posters *schedulePosterStub
		want    string
	}{
		{name: "member omitted", params: map[string]any{}, posters: &schedulePosterStub{}, want: "❌ 멤버 이름을 입력해주세요.\n예) !스케쥴 소피아"},
		{name: "lookup failed", params: map[string]any{paramMember: "미코"}, posters: &schedulePosterStub{err: errors.New("db down")}, want: "❌ 일정 포스터 조회 중 오류가 발생했습니다."},
	} {
		replies := &posterReplies{}
		cards := &scheduleCardStub{data: []byte("card")}
		deps := schedulePosterDeps(t, tc.posters, cards, replies, nil)

		deps.SendError = func(ctx context.Context, _, key string) error {
			replies.messages = append(replies.messages, seeded.ResolveError(ctx, key))

			return nil
		}

		if err := executeSchedulePoster(t, deps, tc.params); err != nil {
			t.Fatalf("%s: Execute returned error: %v", tc.name, err)
		}

		if len(replies.images) != 0 || cards.calls != 0 || !slices.Equal(replies.messages, []string{tc.want}) {
			t.Fatalf("%s: replies = %+v, render calls = %d, want %q", tc.name, replies, cards.calls, tc.want)
		}
	}
}

func TestSchedulePosterCommandRejectsEmptyNotFoundText(t *testing.T) {
	replies := &posterReplies{}

	err := executeSchedulePoster(t, schedulePosterDeps(t, &schedulePosterStub{}, &scheduleCardStub{}, replies, nil), map[string]any{paramMember: "미코"})
	if err == nil || len(replies.messages) != 0 {
		t.Fatalf("Execute = %v, messages = %q; want error and no message", err, replies.messages)
	}
}

func TestSchedulePosterCommandExplainsMissingRowsFromSeededString(t *testing.T) {
	replies := &posterReplies{}
	cards := &scheduleCardStub{data: []byte("card")}
	deps := schedulePosterDeps(t, &schedulePosterStub{}, cards, replies, nil)

	deps.Formatter = newSeededTestFormatter(t)

	if err := executeSchedulePoster(t, deps, map[string]any{paramMember: "미코"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "이번 주 일정표가 아직 없습니다."
	if len(replies.images) != 0 || cards.calls != 0 || !slices.Equal(replies.messages, []string{want}) {
		t.Fatalf("replies = %+v, render calls = %d, want %q", replies, cards.calls, want)
	}
}

func TestSchedulePosterCommandReturnsRenderFailureWithoutAnyReply(t *testing.T) {
	renderErr := errors.New("bad card")
	replies := &posterReplies{}
	deps := schedulePosterDeps(t, &schedulePosterStub{schedule: publishedMikoSchedule(), found: true}, &scheduleCardStub{err: renderErr}, replies, nil)

	err := executeSchedulePoster(t, deps, map[string]any{paramMember: "미코"})
	if !errors.Is(err, renderErr) {
		t.Fatalf("Execute error = %v, want render failure", err)
	}

	if len(replies.images) != 0 || len(replies.messages) != 0 || len(replies.errors) != 0 {
		t.Fatalf("replies = %+v, want no image, text or error reply from the handler", replies)
	}
}

func TestSchedulePosterCommandReturnsSendFailures(t *testing.T) {
	posters := &schedulePosterStub{schedule: publishedMikoSchedule(), found: true}

	err := executeSchedulePoster(t, schedulePosterDeps(t, posters, &scheduleCardStub{data: []byte("card")}, &posterReplies{}, transport.ErrReplyOutcomeUnknown), map[string]any{paramMember: "미코"})
	if !transport.IsReplyOutcomeUnknown(err) {
		t.Fatalf("Execute error = %v, want reply outcome unknown", err)
	}
}

func TestSchedulePosterCommandReturnsMemberLoadFailureWithoutAnyReply(t *testing.T) {
	replies := &posterReplies{}
	cards := &scheduleCardStub{data: []byte("card")}
	posters := &schedulePosterStub{schedule: publishedMikoSchedule(), found: true}
	deps := schedulePosterDeps(t, posters, cards, replies, nil)
	provider := newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelMiko, Name: "Miko", NameKo: "사쿠라 미코", ShortKoreanName: "미코"}})

	clear(provider.byChannel)

	deps.Matcher = matcher.NewMatcher(provider, slog.New(slog.DiscardHandler))

	err := executeSchedulePoster(t, deps, map[string]any{paramMember: "미코"})
	if !errors.Is(err, domain.ErrMemberNotFound) || posters.channelID != testChannelMiko {
		t.Fatalf("Execute error = %v, rows lookup = %q; want member load failure after the rows lookup", err, posters.channelID)
	}

	if cards.calls != 0 || len(replies.images) != 0 || len(replies.messages) != 0 || len(replies.errors) != 0 {
		t.Fatalf("replies = %+v, render calls = %d; want no render and no reply from the handler", replies, cards.calls)
	}
}
