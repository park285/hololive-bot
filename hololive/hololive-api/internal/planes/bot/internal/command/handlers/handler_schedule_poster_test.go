package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
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

type scheduleReplies struct {
	messages, errors []string
	images           [][]byte
	contentTypes     []string
}

func schedulePosterDeps(t *testing.T, holodex *scheduleStreamProviderStub, posters handlercore.SchedulePosterReader, replies *scheduleReplies, imageErr error) *handlercore.Dependencies {
	t.Helper()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{ChannelID: testChannelMiko, Name: "미코"}})

	return &handlercore.Dependencies{
		CollabMembers:   collabMembersStub{},
		Holodex:         holodex,
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
		SendError: func(_ context.Context, _, message string) error {
			replies.errors = append(replies.errors, message)

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}
}

func executeSchedule(t *testing.T, deps *handlercore.Dependencies) error {
	t.Helper()

	return NewScheduleCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, map[string]any{paramMember: "미코"})
}

func TestScheduleCommandAppendsPublishedPoster(t *testing.T) {
	previous := scheduleNow

	t.Cleanup(func() { scheduleNow = previous })

	scheduleNow = func() time.Time { return time.Date(2026, time.November, 10, 16, 30, 0, 0, time.UTC) }

	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}
	replies := &scheduleReplies{}
	holodex := &scheduleStreamProviderStub{scheduleStreams: []*domain.Stream{{ID: "s1", Title: "미코 일정 방송", ChannelID: testChannelMiko}}}

	if err := executeSchedule(t, schedulePosterDeps(t, holodex, posters, replies, nil)); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.messages) != 1 || len(replies.images) != 1 || string(replies.images[0]) != "poster" || replies.contentTypes[0] != testContentTypeJPEG {
		t.Fatalf("replies = %+v, want schedule text then poster image", replies)
	}

	if posters.channelID != testChannelMiko || !posters.day.Equal(time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("poster lookup = %s %s, want JST day 2026-11-11", posters.channelID, posters.day)
	}
}

func TestScheduleCommandSendsPosterAloneWhenHolodexFails(t *testing.T) {
	holodex := &scheduleStreamProviderStub{scheduleErr: errors.New("holodex down")}

	replies := &scheduleReplies{}
	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}

	if err := executeSchedule(t, schedulePosterDeps(t, holodex, posters, replies, nil)); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.images) != 1 || len(replies.errors) != 0 || len(replies.messages) != 0 {
		t.Fatalf("replies = %+v, want poster only", replies)
	}

	replies = &scheduleReplies{}
	if err := executeSchedule(t, schedulePosterDeps(t, holodex, &schedulePosterStub{}, replies, nil)); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(replies.errors) != 1 || replies.errors[0] != messaging.ErrScheduleQueryFailed {
		t.Fatalf("replies = %+v, want schedule query error without poster", replies)
	}
}

func TestScheduleCommandKeepsScheduleWhenPosterFails(t *testing.T) {
	holodex := &scheduleStreamProviderStub{scheduleStreams: []*domain.Stream{{ID: "s1", Title: "미코 일정 방송", ChannelID: testChannelMiko}}}

	for _, posters := range []*schedulePosterStub{
		{err: errors.New("db down")},
		{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true},
	} {
		replies := &scheduleReplies{}
		if err := executeSchedule(t, schedulePosterDeps(t, holodex, posters, replies, errors.New("iris rejected"))); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}

		if len(replies.messages) != 1 || len(replies.errors) != 0 {
			t.Fatalf("replies = %+v, want schedule text only", replies)
		}
	}

	replies := &scheduleReplies{}
	posters := &schedulePosterStub{image: scheduleposter.Image{MIME: testContentTypeJPEG, Data: []byte("poster")}, found: true}

	err := executeSchedule(t, schedulePosterDeps(t, holodex, posters, replies, transport.ErrReplyOutcomeUnknown))
	if !transport.IsReplyOutcomeUnknown(err) {
		t.Fatalf("Execute error = %v, want reply outcome unknown", err)
	}
}
