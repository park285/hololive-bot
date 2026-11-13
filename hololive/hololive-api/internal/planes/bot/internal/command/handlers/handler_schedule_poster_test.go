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
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

type schedulePosterStub struct {
	image     scheduleposter.Image
	found     bool
	err       error
	channelID string
	day       time.Time
}

func (s *schedulePosterStub) PublishedImage(_ context.Context, channelID string, day time.Time) (scheduleposter.Image, bool, error) {
	s.channelID, s.day = channelID, day

	return s.image, s.found, s.err
}

type posterReplies struct {
	messages     []string
	images       [][]byte
	contentTypes []string
}

func schedulePosterDeps(t *testing.T, posters handlercore.SchedulePosterReader, replies *posterReplies, imageErr error) *handlercore.Dependencies {
	t.Helper()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelMiko, Name: "미코"}})

	return &handlercore.Dependencies{
		Matcher:         matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:       formatter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SchedulePosters: posters,
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
		SendError: func(context.Context, string, string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}
}

func executeSchedulePoster(t *testing.T, deps *handlercore.Dependencies, params map[string]any) error {
	t.Helper()

	return NewSchedulePosterCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, params)
}

func TestSchedulePosterCommandSendsThisWeeksPoster(t *testing.T) {
	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}
	replies := &posterReplies{}

	cmd := NewSchedulePosterCommand(schedulePosterDeps(t, posters, replies, nil))

	cmd.now = func() time.Time { return time.Date(2026, time.November, 10, 16, 30, 0, 0, time.UTC) }

	if err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{paramMember: "미코"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.messages) != 0 || len(replies.images) != 1 || string(replies.images[0]) != "poster" || replies.contentTypes[0] != testContentTypeJPEG {
		t.Fatalf("replies = %+v, want the poster image only", replies)
	}

	if posters.channelID != testChannelMiko || !posters.day.Equal(time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("poster lookup = %s %s, want JST day 2026-11-11", posters.channelID, posters.day)
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
		deps := schedulePosterDeps(t, tc.posters, replies, nil)

		deps.SendError = func(ctx context.Context, _, key string) error {
			replies.messages = append(replies.messages, seeded.ResolveError(ctx, key))

			return nil
		}

		if err := executeSchedulePoster(t, deps, tc.params); err != nil {
			t.Fatalf("%s: Execute returned error: %v", tc.name, err)
		}

		if len(replies.images) != 0 || !slices.Equal(replies.messages, []string{tc.want}) {
			t.Fatalf("%s: replies = %+v, want %q", tc.name, replies, tc.want)
		}
	}
}

func TestSchedulePosterCommandRejectsEmptyNotFoundText(t *testing.T) {
	replies := &posterReplies{}

	err := executeSchedulePoster(t, schedulePosterDeps(t, &schedulePosterStub{}, replies, nil), map[string]any{paramMember: "미코"})
	if err == nil || len(replies.messages) != 0 {
		t.Fatalf("Execute = %v, messages = %q; want error and no message", err, replies.messages)
	}
}

func TestSchedulePosterCommandExplainsMissingPosterFromSeededString(t *testing.T) {
	replies := &posterReplies{}
	deps := schedulePosterDeps(t, &schedulePosterStub{}, replies, nil)

	deps.Formatter = newSeededTestFormatter(t)

	if err := executeSchedulePoster(t, deps, map[string]any{paramMember: "미코"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "이번 주 번역된 일정 포스터가 아직 없습니다."
	if len(replies.images) != 0 || !slices.Equal(replies.messages, []string{want}) {
		t.Fatalf("replies = %+v, want %q", replies, want)
	}
}

func TestSchedulePosterCommandReturnsSendFailures(t *testing.T) {
	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}

	err := executeSchedulePoster(t, schedulePosterDeps(t, posters, &posterReplies{}, transport.ErrReplyOutcomeUnknown), map[string]any{paramMember: "미코"})
	if !transport.IsReplyOutcomeUnknown(err) {
		t.Fatalf("Execute error = %v, want reply outcome unknown", err)
	}
}
