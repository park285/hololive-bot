package handlers

import (
	"context"
	"errors"
	"log/slog"
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
	previous := schedulePosterNow

	t.Cleanup(func() { schedulePosterNow = previous })

	schedulePosterNow = func() time.Time { return time.Date(2026, time.November, 10, 16, 30, 0, 0, time.UTC) }

	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}
	replies := &posterReplies{}

	if err := executeSchedulePoster(t, schedulePosterDeps(t, posters, replies, nil), map[string]any{paramMember: "미코"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.messages) != 0 || len(replies.images) != 1 || string(replies.images[0]) != "poster" || replies.contentTypes[0] != testContentTypeJPEG {
		t.Fatalf("replies = %+v, want the poster image only", replies)
	}

	if posters.channelID != testChannelMiko || !posters.day.Equal(time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("poster lookup = %s %s, want JST day 2026-11-11", posters.channelID, posters.day)
	}
}

func TestSchedulePosterCommandExplainsMissingPoster(t *testing.T) {
	for _, tc := range []struct {
		name    string
		params  map[string]any
		posters *schedulePosterStub
		want    string
	}{
		{name: "member omitted", params: map[string]any{}, posters: &schedulePosterStub{}, want: schedulePosterNeedMember},
		{name: "no translated poster this week", params: map[string]any{paramMember: "미코"}, posters: &schedulePosterStub{}, want: schedulePosterNotFound},
		{name: "lookup failed", params: map[string]any{paramMember: "미코"}, posters: &schedulePosterStub{err: errors.New("db down")}, want: schedulePosterQueryFailed},
	} {
		replies := &posterReplies{}
		if err := executeSchedulePoster(t, schedulePosterDeps(t, tc.posters, replies, nil), tc.params); err != nil {
			t.Fatalf("%s: Execute returned error: %v", tc.name, err)
		}

		if len(replies.images) != 0 || len(replies.messages) != 1 || replies.messages[0] != tc.want {
			t.Fatalf("%s: replies = %+v, want %q", tc.name, replies, tc.want)
		}
	}
}

func TestSchedulePosterCommandReturnsSendFailures(t *testing.T) {
	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}

	err := executeSchedulePoster(t, schedulePosterDeps(t, posters, &posterReplies{}, transport.ErrReplyOutcomeUnknown), map[string]any{paramMember: "미코"})
	if !transport.IsReplyOutcomeUnknown(err) {
		t.Fatalf("Execute error = %v, want reply outcome unknown", err)
	}
}
