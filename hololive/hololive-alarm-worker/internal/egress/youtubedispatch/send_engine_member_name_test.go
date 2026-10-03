package youtubedispatch

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/format"
	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestDispatchDeliveryRows_GroupedFormatFailureRetriesWholeGroup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		memberNames format.MemberNameSource
		groupBody   string
	}{
		// 표시명 정본 조회가 실패한다. 대체 문구로 보내지 않는다.
		{name: "member name lookup failed", memberNames: failingMemberNames{err: errors.New("db down")}, groupBody: "{{range .Items}}{{.Title}} {{.URL}}\n{{end}}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sender := &testSender{failRoom: map[string]bool{}}
			renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShorts, "{{.Title}}\n{{.URL}}")

			if tc.groupBody != "" {
				renderer = newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShortsGroup, tc.groupBody)
			}

			d := newDispatcherWithDepsForTest(t, nil, Dependencies{
				Cache: cachemocks.NewLenientClient(), Sender: sender, Renderer: renderer, MemberNames: tc.memberNames,
			}, slog.New(slog.DiscardHandler), &dispatchstate.Config{BatchSize: 10, DeliveryParallelism: 2})

			outboxByID := map[int64]domain.YouTubeNotificationOutbox{
				1: {ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortOne, Payload: testPayloadShortOne},
				2: {ID: 2, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortTwo, Payload: testPayloadShortTwo},
			}
			rows := []domain.YouTubeNotificationDelivery{
				{ID: 101, OutboxID: 1, RoomID: testRoom1},
				{ID: 102, OutboxID: 2, RoomID: testRoom1},
			}

			result := d.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

			if len(result.SuccessDeliveryIDs) != 0 {
				t.Fatalf("successDeliveryIDs = %v, want none", result.SuccessDeliveryIDs)
			}

			sender.mu.Lock()

			msgCount := len(sender.messages)
			sender.mu.Unlock()

			if msgCount != 0 {
				t.Fatalf("sender message count = %d, want 0 (no individual fallback)", msgCount)
			}

			spy, ok := d.send.transition.(*lifecycleTransitionSpy)
			if !ok {
				t.Fatalf("transition = %T, want lifecycle spy", d.send.transition)
			}

			want := []string{fmt.Sprintf("%v/%v/%v", ytlifecycle.FailureRetryable, lifecycleReasonFormat, store.DeliveryModeGrouped)}
			if got := spy.recordedPreparedFailures(); !slices.Equal(got, want) {
				t.Fatalf("prepared failures = %v, want %v", got, want)
			}
		})
	}
}

// dispatchOnePerRoomShort는 표시명 원천만 바꿔 쇼츠 한 건을 한 방에 개별 발송하고, 보낸 메시지와 결과를 돌려준다.
func dispatchOnePerRoomShort(t *testing.T, memberNames format.MemberNameSource) (*Dispatcher, []string, dispatchstate.DispatchResult) {
	t.Helper()

	sender := &testSender{failRoom: map[string]bool{}}
	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShorts, "{{.MemberName}}|{{.URL}}")
	d := newDispatcherWithDepsForTest(t, nil, Dependencies{
		Cache: cachemocks.NewLenientClient(), Sender: sender, Renderer: renderer, MemberNames: memberNames,
	}, slog.New(slog.DiscardHandler), &dispatchstate.Config{BatchSize: 10, DeliveryParallelism: 2})

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortOne, Payload: testPayloadShortOne},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: testRoom1}}

	result := d.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	sender.mu.Lock()
	defer sender.mu.Unlock()

	return d, slices.Clone(sender.messages), result
}

func TestDispatchDeliveryRows_PerRoomMemberNameComesFromCanonicalSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		memberNames format.MemberNameSource
		// wantName이 비어 있으면 misc/vtuber_fallback 문구를 기대한다.
		wantName string
	}{
		{name: "registered name", memberNames: staticMemberNames{testChannelCh1: "페코라"}, wantName: "페코라"},
		{name: "missing name uses fallback string", memberNames: staticMemberNames{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d, messages, _ := dispatchOnePerRoomShort(t, tc.memberNames)

			wantName := cmp.Or(tc.wantName, d.send.formatter.DisplayMemberName(""))
			if wantName == "" {
				t.Fatal("misc/vtuber_fallback string is not loaded")
			}

			if len(messages) != 1 || !strings.HasPrefix(messages[0], testRoom1+":"+wantName+"|") {
				t.Fatalf("messages = %v, want one message starting with %q", messages, wantName)
			}
		})
	}
}

// 표시명 정본 조회가 실패하면 대체 문구로 보내지 않고 재시도 가능한 포맷 실패로 전이한다.
func TestDispatchDeliveryRows_PerRoomMemberNameLookupFailureRetriesWithoutSending(t *testing.T) {
	t.Parallel()

	d, messages, result := dispatchOnePerRoomShort(t, failingMemberNames{err: errors.New("db down")})

	if len(messages) != 0 || len(result.SuccessDeliveryIDs) != 0 {
		t.Fatalf("messages = %v, successes = %v, want no delivery", messages, result.SuccessDeliveryIDs)
	}

	spy, ok := d.send.transition.(*lifecycleTransitionSpy)
	if !ok {
		t.Fatalf("transition = %T, want lifecycle spy", d.send.transition)
	}

	want := []string{fmt.Sprintf("%v/%v/%v", ytlifecycle.FailureRetryable, lifecycleReasonFormat, store.DeliveryModePerRoom)}
	if got := spy.recordedPreparedFailures(); !slices.Equal(got, want) {
		t.Fatalf("prepared failures = %v, want %v", got, want)
	}
}
