package youtubejscollector

import (
	"context"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

const liveCheckTestVideoID = "video-live-a"

// 방송 탭 job과 채널 확인 job은 서로의 RPC를 보내지 않고 자기 kind만 방출한다.
func TestChannelLiveJobsEmitOnlyOwnKind(t *testing.T) {
	t.Parallel()

	fake := &channelFake{check: youtubejs.ChannelLiveCheckResult{
		ChannelID: restrictedTestChannelID, Outcome: contract.ChannelLiveCheckUpcomingVideo,
		SelectedVideoID: "upcoming-a", ChannelIdentityConfirmed: true,
	}}

	loadJSON(t, "channel.json", &fake.result)

	snapshot, err := NewChannelLiveRunner(fake).Collect(t.Context(), channelLiveInput(t, 1))
	if err != nil {
		t.Fatal(err)
	}

	snapshots := snapshot.Output().Observations()
	if snapshot.Kind() != collectutil.CollectComplete || len(snapshots) != 1 ||
		snapshots[0].ObservationKind != contract.KindLiveSnapshot || fake.checkCalls != 0 {
		t.Fatalf("snapshot job result = %#v check calls=%d", snapshots, fake.checkCalls)
	}

	result, err := NewChannelLiveCheckRunner(fake).Collect(t.Context(), channelLiveCheckInput(t))
	if err != nil {
		t.Fatal(err)
	}

	observations := result.Output().Observations()
	if result.Kind() != collectutil.CollectComplete || len(observations) != 1 ||
		observations[0].ObservationKind != contract.KindChannelLiveCheck || fake.calls != 1 || fake.checkCalls != 1 {
		t.Fatalf("check job result = %#v channel calls=%d check calls=%d", observations, fake.calls, fake.checkCalls)
	}

	check := observations[0]
	payload := decodeChannelLiveCheck(t, &check)

	if check.Completeness != contract.CompletenessComplete || check.Continuity != contract.ContinuityNotApplicable ||
		payload.Coverage.ChannelID != restrictedTestChannelID || payload.SelectedVideoID != "upcoming-a" || !payload.NegativeLiveEvidence() {
		t.Fatalf("channel live check = %#v payload=%#v", check, payload)
	}
}

// 방송 탭 실패는 빈 snapshot이나 부분 결과로 바꾸지 않고 job 실패로 남아 해당 슬롯만 재시도한다.
func TestChannelLiveRunnerDoesNotPublishFailedSnapshot(t *testing.T) {
	t.Parallel()

	fake := &channelFake{err: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "channel tab timeout")}

	result, err := NewChannelLiveRunner(fake).Collect(t.Context(), channelLiveInput(t, 1))
	if err == nil || collecterr.ClassOf(err) != collecterr.ClassTimeout || !result.IsZero() || fake.checkCalls != 0 {
		t.Fatalf("failed snapshot result=%#v err=%v check calls=%d", result, err, fake.checkCalls)
	}
}

func TestChannelLiveCheckRunnerRecordsRequestFailureAsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		check    youtubejs.ChannelLiveCheckResult
		checkErr error
	}{
		{"transient", youtubejs.ChannelLiveCheckResult{}, collecterr.New(collecterr.Failed, collecterr.ClassTransient, "upstream failed")},
		{"cooldown", youtubejs.ChannelLiveCheckResult{}, collecterr.New(collecterr.Cooldown, collecterr.ClassCooldown, "limited")},
		{"protocol mismatch", youtubejs.ChannelLiveCheckResult{}, collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "mismatch")},
		{"invalid negative shape", youtubejs.ChannelLiveCheckResult{ChannelID: restrictedTestChannelID, Outcome: contract.ChannelLiveCheckUpcomingVideo, ChannelIdentityConfirmed: true}, nil},
		{"other subject", channelPageCheck("UC_OTHER"), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := channelLiveCheckInput(t)

			result, err := NewChannelLiveCheckRunner(&channelFake{check: tt.check, checkErr: tt.checkErr}).Collect(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}

			observations := result.Output().Observations()
			if result.Kind() != collectutil.CollectComplete || len(observations) != 1 {
				t.Fatalf("result = %#v", result)
			}

			check := observations[0]
			payload := decodeChannelLiveCheck(t, &check)

			if check.Completeness != contract.CompletenessUnknown || payload.Outcome != contract.ChannelLiveCheckUnknown ||
				payload.UnknownReason != contract.LiveCheckReasonRequestFailed || payload.ChannelIdentityConfirmed ||
				payload.SelectedVideoID != "" || payload.ChannelID != restrictedTestChannelID ||
				!check.ScheduledFor.Equal(input.Lease().ScheduledFor) {
				t.Fatalf("request failure observation = %#v payload=%#v", check, payload)
			}
		})
	}
}

func TestChannelLiveCheckRunnerDoesNotPublishCanceledOrFatalCheck(t *testing.T) {
	t.Parallel()

	for _, tt := range stopCheckCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := channelLiveCheckInput(t)
			ctx := tt.newContext(t)

			result, err := NewChannelLiveCheckRunner(&channelFake{checkErr: tt.err}).Collect(ctx, input)
			if err == nil || !result.IsZero() {
				t.Fatalf("stopped check was published: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestVideoLiveCheckRunnerPublishesFactsForRequestedSubject(t *testing.T) {
	t.Parallel()

	endedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	startedAt := endedAt.Add(-2 * time.Hour)
	fake := &videoLiveFake{result: youtubejs.VideoLiveCheckResult{
		VideoID: liveCheckTestVideoID, ChannelID: restrictedTestChannelID, IdentityConfirmed: true,
		IsLiveNow: new(false), IsLiveContent: new(true), IsPrivate: new(false), HasLiveBroadcastDetails: new(true),
		StartedAt: &startedAt, EndedAt: &endedAt,
		Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic,
	}}

	result, err := NewVideoLiveCheckRunner(fake).Collect(t.Context(), videoLiveInput(t))
	if err != nil {
		t.Fatal(err)
	}

	observations := result.Output().Observations()
	if result.Kind() != collectutil.CollectComplete || len(observations) != 1 || fake.request.VideoID != liveCheckTestVideoID {
		t.Fatalf("result = %#v request=%#v", result, fake.request)
	}

	observation := observations[0]
	payload := decodeVideoLiveCheck(t, &observation)

	verifiedEnd, ok := payload.VerifiedEndedAt()
	if observation.Completeness != contract.CompletenessPartial || observation.Continuity != contract.ContinuityNotApplicable ||
		payload.IsLive != nil || payload.IsUpcoming != nil || payload.Coverage.VideoID != liveCheckTestVideoID ||
		!ok || !verifiedEnd.Equal(endedAt) {
		t.Fatalf("video live check = %#v payload=%#v", observation, payload)
	}
}

func TestVideoLiveCheckRunnerRecordsRequestFailureAsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result youtubejs.VideoLiveCheckResult
		err    error
	}{
		{"transient", youtubejs.VideoLiveCheckResult{}, collecterr.New(collecterr.Failed, collecterr.ClassTransient, "upstream failed")},
		{"request timeout", youtubejs.VideoLiveCheckResult{}, collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "helper timeout")},
		{"response too large", youtubejs.VideoLiveCheckResult{}, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "too large")},
		{"protocol mismatch", youtubejs.VideoLiveCheckResult{}, collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "mismatch")},
		{"invalid public fact", youtubejs.VideoLiveCheckResult{VideoID: liveCheckTestVideoID, ChannelID: restrictedTestChannelID, IdentityConfirmed: true, Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic}, nil},
		{"other subject", youtubejs.VideoLiveCheckResult{
			VideoID: "video-other", ChannelID: restrictedTestChannelID, IdentityConfirmed: true, IsPrivate: new(false),
			Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic,
		}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := videoLiveInput(t)
			before := time.Now().UTC()

			result, err := NewVideoLiveCheckRunner(&videoLiveFake{result: tt.result, err: tt.err}).Collect(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}

			observations := result.Output().Observations()
			if result.Kind() != collectutil.CollectComplete || len(observations) != 1 {
				t.Fatalf("result = %#v", result)
			}

			observation := observations[0]
			payload := decodeVideoLiveCheck(t, &observation)
			failed := contract.VideoLiveCheckV1{
				VideoID: liveCheckTestVideoID, Availability: contract.VideoAvailabilityUnknown,
				Method: contract.VideoAvailabilityMethodUnknown, UnknownReason: contract.LiveCheckReasonRequestFailed,
				Coverage: contract.VideoLiveCheckCoverageV1{VideoID: liveCheckTestVideoID},
			}

			// 요청 실패는 채널을 추정하지 않고 관측 시각의 사실 없는 UNKNOWN으로 남는다.
			if observation.Completeness != contract.CompletenessUnknown || observation.ObservedAt.Before(before) ||
				!observation.ScheduledFor.Equal(input.Lease().ScheduledFor) || !reflect.DeepEqual(payload, failed) {
				t.Fatalf("request failure observation = %#v payload=%#v", observation, payload)
			}
		})
	}
}

func TestVideoLiveCheckRunnerDoesNotPublishCanceledOrFatalCheck(t *testing.T) {
	t.Parallel()

	for _, tt := range stopCheckCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := videoLiveInput(t)
			ctx := tt.newContext(t)

			result, err := NewVideoLiveCheckRunner(&videoLiveFake{err: tt.err}).Collect(ctx, input)
			if err == nil || !result.IsZero() {
				t.Fatalf("stopped check was published: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestVideoLiveCheckRunnerSkipsDisabledTarget(t *testing.T) {
	t.Parallel()

	fake := &videoLiveFake{}
	input := withEnabled(t, videoLiveInput(t), map[contract.ObservationKind][]string{contract.KindVideoLiveCheck: {}})

	result, err := NewVideoLiveCheckRunner(fake).Collect(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}

	if fake.calls != 0 || !result.Output().Empty() {
		t.Fatalf("disabled target fetched=%d output=%#v", fake.calls, result.Output().Observations())
	}
}

type stopCheckCase struct {
	name       string
	newContext func(*testing.T) context.Context
	err        error
}

// stopCheckCases는 관측으로 바꾸면 안 되는 실패다. 수집 context 종료는 원인 분류와 무관하게 발행을 막는다.
func stopCheckCases() []stopCheckCase {
	live := func(t *testing.T) context.Context {
		t.Helper()

		return t.Context()
	}

	return []stopCheckCase{
		{"collection canceled", func(t *testing.T) context.Context {
			t.Helper()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			return ctx
		}, collecterr.FromContext(context.Canceled)},
		{"lease deadline", func(t *testing.T) context.Context {
			t.Helper()

			ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			t.Cleanup(cancel)

			return ctx
		}, collecterr.FromContext(context.DeadlineExceeded)},
		{"helper canceled", live, collecterr.New(collecterr.Canceled, collecterr.ClassCanceled, "helper canceled")},
		{"internal invariant", live, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "broken")},
	}
}

func videoLiveInput(tb testing.TB) *collectutil.RunInput {
	tb.Helper()

	return youtubeInput(tb, liveCheckTestVideoID, "youtubejs_video_live", contract.KindVideoLiveCheck)
}

func channelLiveCheckInput(tb testing.TB) *collectutil.RunInput {
	tb.Helper()

	return youtubeInput(tb, restrictedTestChannelID, "youtubejs_channel_live_check", contract.KindChannelLiveCheck)
}

func decodeChannelLiveCheck(t *testing.T, envelope *contract.Envelope) contract.ChannelLiveCheckV1 {
	t.Helper()

	var payload contract.ChannelLiveCheckV1

	if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	return payload
}

func decodeVideoLiveCheck(t *testing.T, envelope *contract.Envelope) contract.VideoLiveCheckV1 {
	t.Helper()

	var payload contract.VideoLiveCheckV1

	if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	return payload
}

type videoLiveFake struct {
	result  youtubejs.VideoLiveCheckResult
	err     error
	calls   int
	request youtubejs.VideoLiveCheckRequest
}

func (f *videoLiveFake) FetchVideoLiveCheck(_ context.Context, request youtubejs.VideoLiveCheckRequest) (youtubejs.VideoLiveCheckResult, error) {
	f.calls++

	f.request = request

	return f.result, f.err
}
