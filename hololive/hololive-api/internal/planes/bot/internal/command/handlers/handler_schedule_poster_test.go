package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v6/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type schedulePosterStub struct {
	selected  cardtemplate.ID
	memberID  int
	schedule  scheduleposter.PublishedSchedule
	found     bool
	err       error
	channelID string
	day       time.Time
}

func (s *schedulePosterStub) PublishedMemberRows(_ context.Context, memberID int, channelID string, day time.Time) (scheduleposter.PublishedSchedule, cardtemplate.ID, bool, error) {
	s.memberID, s.channelID, s.day = memberID, channelID, day

	return s.schedule, s.selected, s.found, s.err
}

type scheduleCardStub struct {
	data     [][]byte
	template cardtemplate.ID
	err      error
	member   *domain.Member
	schedule scheduleposter.PublishedSchedule
	calls    int
}

func (s *scheduleCardStub) RenderScheduleCards(_ context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID) ([][]byte, error) {
	s.calls++

	s.member, s.schedule, s.template = member, schedule, template

	return s.data, s.err
}

type posterReplies struct {
	messages     []string
	errors       []string
	images       [][]byte
	contentTypes []string
	batchSizes   []int
	files        []string
}

func schedulePosterDeps(t *testing.T, posters handlercore.SchedulePosterReader, cards handlercore.ScheduleCardRenderer, replies *posterReplies, imageErr error) *handlercore.Dependencies {
	t.Helper()

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{ID: 37, ChannelID: testChannelMiko, Name: "Miko", NameKo: "사쿠라 미코", ShortKoreanName: "미코"}})

	return &handlercore.Dependencies{
		Matcher:         matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:       formatter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SchedulePosters: posters,
		ScheduleCards:   cards,
		SendMessage: func(_ context.Context, _, message string) error {
			replies.messages = append(replies.messages, message)

			return nil
		},
		SendTextFile: func(_ context.Context, _, name, text string) error {
			if name != "schedule.txt" {
				t.Fatalf("file name: %s", name)
			}

			replies.files = append(replies.files, text)

			return imageErr
		},
		SendImages: func(ctx context.Context, _ string, images [][]byte) error {
			contentType, _ := transport.ImageContentTypeFromContext(ctx)

			replies.images = append(replies.images, images...)
			replies.batchSizes = append(replies.batchSizes, len(images))
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

func executeSchedulePoster(t *testing.T, deps *handlercore.Dependencies, params botcommand.SchedulePosterParams) error {
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
	cards := &scheduleCardStub{data: [][]byte{[]byte("card")}}
	replies := &posterReplies{}

	cmd := NewSchedulePosterCommand(schedulePosterDeps(t, posters, cards, replies, nil))

	cmd.now = func() time.Time { return time.Date(2026, time.November, 10, 16, 30, 0, 0, time.UTC) }

	if err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.SchedulePosterParams{Member: "미코"}); err != nil {
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
		params  botcommand.SchedulePosterParams
		posters *schedulePosterStub
		want    string
	}{
		{name: "member omitted", params: botcommand.SchedulePosterParams{}, posters: &schedulePosterStub{}, want: "❌ 멤버 이름을 입력해주세요.\n예) !스케쥴 소피아"},
		{name: "lookup failed", params: botcommand.SchedulePosterParams{Member: "미코"}, posters: &schedulePosterStub{err: errors.New("db down")}, want: "❌ 일정 포스터 조회 중 오류가 발생했습니다."},
	} {
		replies := &posterReplies{}
		cards := &scheduleCardStub{}
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

	err := executeSchedulePoster(t, schedulePosterDeps(t, &schedulePosterStub{}, &scheduleCardStub{}, replies, nil), botcommand.SchedulePosterParams{Member: "미코"})
	if err == nil || len(replies.messages) != 0 {
		t.Fatalf("Execute = %v, messages = %q; want error and no message", err, replies.messages)
	}
}

func TestSchedulePosterCommandExplainsMissingRowsFromSeededString(t *testing.T) {
	replies := &posterReplies{}
	cards := &scheduleCardStub{}
	deps := schedulePosterDeps(t, &schedulePosterStub{}, cards, replies, nil)

	deps.Formatter = newSeededTestFormatter(t)

	if err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "미코"}); err != nil {
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

	err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "미코"})
	if !errors.Is(err, renderErr) {
		t.Fatalf("Execute error = %v, want render failure", err)
	}

	if len(replies.images) != 0 || len(replies.messages) != 0 || len(replies.errors) != 0 {
		t.Fatalf("replies = %+v, want no image, text or error reply from the handler", replies)
	}
}

func TestSchedulePosterCommandReturnsSendFailures(t *testing.T) {
	posters := &schedulePosterStub{schedule: publishedMikoSchedule(), found: true}

	err := executeSchedulePoster(t, schedulePosterDeps(t, posters, &scheduleCardStub{data: [][]byte{[]byte("card")}}, &posterReplies{}, transport.ErrReplyOutcomeUnknown), botcommand.SchedulePosterParams{Member: "미코"})
	if !transport.IsReplyOutcomeUnknown(err) {
		t.Fatalf("Execute error = %v, want reply outcome unknown", err)
	}
}

func TestSchedulePosterUsesNamedMemberOnSharedChannel(t *testing.T) {
	first := &domain.Member{ID: 19, ChannelID: testChannelMiko, Name: "Group", IsGraduated: true}
	second := &domain.Member{ID: 22, ChannelID: testChannelMiko, Name: "Other", NameKo: "유키", Photo: "yuki.png"}
	provider := newContextAwareMemberProvider([]*domain.Member{first, second})

	provider.byChannel[testChannelMiko] = first

	cards := &scheduleCardStub{data: [][]byte{[]byte("image")}}
	posters := &schedulePosterStub{schedule: publishedMikoSchedule(), found: true, selected: cardtemplate.Orbit}
	replies := &posterReplies{}
	deps := schedulePosterDeps(t, posters, cards, replies, nil)

	deps.Matcher = matcher.NewMatcher(provider, slog.New(slog.DiscardHandler))

	if err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "유키"}); err != nil {
		t.Fatal(err)
	}

	if posters.memberID != second.ID || cards.member == nil || cards.member.Photo != second.Photo || cards.template != cardtemplate.Orbit || len(replies.images) != 1 {
		t.Fatalf("lookup ID=%d, rendered=%+v, replies=%+v", posters.memberID, cards.member, replies)
	}

	if err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "Group"}); err != nil {
		t.Fatal(err)
	}

	if cards.calls != 1 || !slices.Equal(replies.errors, []string{messaging.ErrGraduatedMemberBlocked}) {
		t.Fatalf("graduated member was not blocked: %+v", replies)
	}
}

func TestSchedulePosterCommandSwitchesMemberTemplateAndSendsPagesInOrder(t *testing.T) {
	for _, tc := range []struct {
		label    string
		template cardtemplate.ID
		pages    int
		batches  []int
		mime     []string
	}{
		{"기존", cardtemplate.Classic, 1, []int{1}, []string{"image/png"}},
		{"궤도", cardtemplate.Orbit, 8, []int{8}, []string{""}},
	} {
		cards := &scheduleCardStub{}

		for n := range tc.pages {
			cards.data = append(cards.data, []byte{byte(n)})
		}

		replies := &posterReplies{}
		deps := schedulePosterDeps(t, &schedulePosterStub{schedule: publishedMikoSchedule(), found: true, selected: tc.template}, cards, replies, nil)
		command := messaging.NewMessageAdapter("!").ParseMessage(&webhook.Message{Msg: "!스케쥴 사쿠라 미코"})

		if command == nil || command.Type != botcommand.CommandSchedulePoster {
			t.Fatalf("%s: not a schedule poster command", tc.label)
		}

		params, ok := command.Params.(botcommand.SchedulePosterParams)
		if !ok {
			t.Fatalf("%s: params = %T", tc.label, command.Params)
		}

		err := executeSchedulePoster(t, deps, params)

		if err != nil || cards.member == nil || cards.member.ChannelID != testChannelMiko || cards.template != tc.template {
			t.Fatalf("%s: member = %+v, template = %q, err = %v", tc.label, cards.member, cards.template, err)
		}

		if !slices.Equal(replies.batchSizes, tc.batches) || !slices.Equal(replies.contentTypes, tc.mime) {
			t.Fatalf("%s: batches = %v, content types = %v", tc.label, replies.batchSizes, replies.contentTypes)
		}

		for i, page := range replies.images {
			if !slices.Equal(page, cards.data[i]) {
				t.Fatalf("%s: page %d = %v, want %v", tc.label, i+1, page, cards.data[i])
			}
		}
	}
}

func TestSchedulePosterSendsDenseScheduleAsOneCompleteFile(t *testing.T) {
	schedule := publishedMikoSchedule()
	row := schedule.Rows[0]

	schedule.Rows = nil

	for i := range scheduleposter.MaxRows {
		row.Ordinal = i + 1
		row.TitleKo = fmt.Sprintf("%03d", i) + strings.Repeat("일", 997)
		row.NoteKo = strings.Repeat("비", 997) + fmt.Sprintf("%03d", i)
		row.PlaceKo = "장소"
		row.WithKo = []string{"고객"}
		row.TagsKo = []string{"실황"}
		schedule.Rows = append(schedule.Rows, row)
	}

	replies := &posterReplies{}
	deps := schedulePosterDeps(t, &schedulePosterStub{found: true, selected: cardtemplate.Orbit, schedule: schedule}, render.NewScheduleCardRenderer(nil), replies, nil)

	if err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "미코"}); err != nil {
		t.Fatal(err)
	}

	if len(replies.files) != 1 || len(replies.images) != 0 || len(replies.messages) != 0 {
		t.Fatalf("files=%d, images=%d, messages=%d", len(replies.files), len(replies.images), len(replies.messages))
	}

	text := replies.files[0]

	for _, row := range schedule.Rows {
		if !strings.Contains(text, row.TitleKo) || !strings.Contains(text, row.NoteKo) {
			t.Fatalf("incomplete row %d", row.Ordinal)
		}
	}

	for _, value := range []string{"장소: 장소", "함께: 고객", "태그: 실황"} {
		if strings.Count(text, value) != scheduleposter.MaxRows {
			t.Fatalf("missing %s", value)
		}
	}
}

func TestSchedulePosterFileFailureDoesNotSendAnImage(t *testing.T) {
	replies := &posterReplies{}
	deps := schedulePosterDeps(t, &schedulePosterStub{found: true, schedule: publishedMikoSchedule()}, &scheduleCardStub{err: schedulecard.ErrRenderBudget}, replies, transport.ErrReplyOutcomeUnknown)
	err := executeSchedulePoster(t, deps, botcommand.SchedulePosterParams{Member: "미코"})

	if !transport.IsReplyOutcomeUnknown(err) || len(replies.files) != 1 || len(replies.images) != 0 {
		t.Fatalf("err=%v, replies=%+v", err, replies)
	}
}
