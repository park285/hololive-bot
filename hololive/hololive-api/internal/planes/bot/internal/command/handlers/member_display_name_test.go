package handlers

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	responseformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	alarmcmd "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/alarm"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

// 같은 멤버를 영어 이름으로 찾아도 명령마다 정본 표시명(short_korean_name)을 써야 한다.
const (
	testChannelSora      = "ch-sora"
	displayNameQuery     = "Tokino Sora"
	canonicalDisplayName = "소라"
)

type liveQueryRequestRecorder struct {
	request livequery.Request
}

func (s *liveQueryRequestRecorder) Query(_ context.Context, request livequery.Request) (livequery.Result, error) {
	s.request = request

	return livequery.Result{}, nil
}

// runDisplayNameCommand는 운영 seed 템플릿과 message_strings로 명령을 실행해 실제 응답 문구를 돌려준다.
func runDisplayNameCommand(t *testing.T, deps *handlercore.Dependencies, execute func(*handlercore.Dependencies) error) string {
	t.Helper()

	pool := dbtest.NewPool(t)
	store := messagestrings.NewStore(pool, slog.New(slog.DiscardHandler))

	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	var sent []string

	deps.Matcher = matcher.NewMatcher(newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelSora, Name: displayNameQuery, NameKo: "토키노 소라", ShortKoreanName: canonicalDisplayName, Org: "Hololive",
	}}), nil, slog.New(slog.DiscardHandler))
	deps.Formatter = responseformatter.NewResponseFormatter("!", serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler)), responseformatter.WithMessageStrings(store))
	deps.SendMessage = func(_ context.Context, _, message string) error {
		sent = append(sent, message)

		return nil
	}
	deps.SendError = func(_ context.Context, _, message string) error {
		t.Fatalf("unexpected error reply: %s", message)

		return nil
	}
	deps.Logger = slog.New(slog.DiscardHandler)

	if err := execute(deps); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	return strings.Join(sent, "\n")
}

func assertCanonicalDisplayName(t *testing.T, message string) {
	t.Helper()

	if !strings.Contains(message, canonicalDisplayName) || strings.Contains(message, displayNameQuery) {
		t.Fatalf("message = %q, want %q without english name", message, canonicalDisplayName)
	}
}

func displayNameCommandContext() *domain.CommandContext {
	return &domain.CommandContext{Room: testRoomID}
}

func TestLiveMemberQueryUsesCanonicalDisplayName(t *testing.T) {
	recorder := &liveQueryRequestRecorder{}
	runDisplayNameCommand(t, &handlercore.Dependencies{LiveQuery: recorder}, func(deps *handlercore.Dependencies) error {
		return NewLiveCommand(deps).Execute(t.Context(), displayNameCommandContext(), map[string]any{paramMember: displayNameQuery})
	})

	if recorder.request.MemberName != canonicalDisplayName {
		t.Fatalf("live query member name = %q, want %q", recorder.request.MemberName, canonicalDisplayName)
	}
}

func TestMemberNoUpcomingUsesCanonicalDisplayName(t *testing.T) {
	assertCanonicalDisplayName(t, runDisplayNameCommand(t, &handlercore.Dependencies{Holodex: &upcomingStreamProviderStub{}}, func(deps *handlercore.Dependencies) error {
		return NewUpcomingCommand(deps).Execute(t.Context(), displayNameCommandContext(), map[string]any{paramMember: displayNameQuery})
	}))
}

func TestScheduleTitleUsesCanonicalDisplayName(t *testing.T) {
	start := time.Now().Add(time.Hour)
	holodex := &scheduleStreamProviderStub{scheduleStreams: []*domain.Stream{{
		ID: "video-1", ChannelID: testChannelSora, ChannelName: "Sora Ch. ときのそら", Title: "예정 방송", StartScheduled: &start,
	}}}

	assertCanonicalDisplayName(t, runDisplayNameCommand(t, &handlercore.Dependencies{Holodex: holodex}, func(deps *handlercore.Dependencies) error {
		return NewScheduleCommand(deps).Execute(t.Context(), displayNameCommandContext(), map[string]any{paramMember: displayNameQuery})
	}))
}

func TestAlarmReplyUsesCanonicalDisplayName(t *testing.T) {
	for _, action := range []string{testActionAdd, "remove"} {
		t.Run(action, func(t *testing.T) {
			assertCanonicalDisplayName(t, runDisplayNameCommand(t, &handlercore.Dependencies{Alarm: &alarmListViewerStub{}}, func(deps *handlercore.Dependencies) error {
				return alarmcmd.NewAlarmCommand(deps).Execute(t.Context(), displayNameCommandContext(), map[string]any{testParamAction: action, paramMember: displayNameQuery})
			}))
		})
	}
}

func TestBroadcastHistoryFilterUsesCanonicalDisplayName(t *testing.T) {
	message := runDisplayNameCommand(t, &handlercore.Dependencies{BroadcastHistory: &stubBroadcastHistoryRepository{}}, func(deps *handlercore.Dependencies) error {
		return NewBroadcastHistoryCommand(deps).Execute(t.Context(), displayNameCommandContext(), map[string]any{paramMember: displayNameQuery})
	})

	if !strings.Contains(message, "멤버: "+canonicalDisplayName) {
		t.Fatalf("message = %q, want member filter %q", message, canonicalDisplayName)
	}
}
