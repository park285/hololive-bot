package holodexcollector

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"slices"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/testutil"
)

const (
	channelA = "UC_A"
	channelB = "UC_B"
	channelC = "UC_C"
)

func TestRunnerBuildsOneBatchFromLiveFixture(t *testing.T) {
	t.Parallel()

	output := mustCollect(t, testdata(t, "live.json"), []string{channelA, channelB, channelC})
	observations := output.Observations()

	if len(observations) != 2 {
		t.Fatalf("observations = %d", len(observations))
	}

	kinds := map[contract.ObservationKind]int{}
	subjects := map[string]struct{}{}

	for _, envelope := range observations {
		kinds[envelope.ObservationKind]++

		subjects[envelope.SubjectKey+"/"+string(envelope.ObservationKind)] = struct{}{}

		if envelope.Completeness != contract.CompletenessPartial {
			t.Fatalf("completeness = %s", envelope.Completeness)
		}
	}

	if _, ok := subjects["UC_C/"+string(contract.KindLiveSnapshot)]; ok {
		t.Fatal("POSITIVE_ONLY must not emit an empty snapshot for a missing requested channel")
	}

	if kinds[contract.KindLiveSnapshot] != 2 {
		t.Fatalf("live snapshots = %d", kinds[contract.KindLiveSnapshot])
	}
}

func TestRunnerIgnoresViewerCountsForLiveAndSchedule(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"live-a","title":"Live","status":"live","channel_id":"UC_A","start_scheduled":"2026-08-14T10:00:00Z","start_actual":"2026-08-14T10:01:00Z","live_viewers":-1},
		{"id":"soon-b","title":"Soon","status":"upcoming","channel_id":"UC_B","start_scheduled":"2026-08-14T12:00:00Z","live_viewers":"hidden"},
		{"id":"live-c","title":"Live C","status":"live","channel_id":"UC_C","start_scheduled":"2026-08-14T09:00:00Z","live_viewers":{"unexpected":true}}
	]`)
	requested := []string{channelA, channelB, channelC}
	output := mustCollect(t, body, requested)
	observations := output.Observations()

	if len(observations) != 3 {
		t.Fatalf("live observations = %#v", observations)
	}

	for _, envelope := range observations {
		assertLiveScopeWithoutViewers(t, envelope)
	}

	for _, checkpoint := range output.Checkpoints() {
		if checkpoint.ObservationKind != contract.KindLiveSnapshot {
			t.Fatalf("unexpected checkpoint = %#v", checkpoint)
		}
	}

	schedule, err := NewScheduleRunner(&staticFetcher{body: body}).Collect(
		t.Context(), holodexInputFor(t, "holodex_schedule", requested),
	)
	if err != nil {
		t.Fatal(err)
	}

	scheduleObservations := schedule.Output().Observations()
	if len(scheduleObservations) != 1 || scheduleObservations[0].ObservationKind != contract.KindSchedule {
		t.Fatalf("schedule observations = %#v", scheduleObservations)
	}

	var payload contract.ScheduleSnapshotV1

	if err := jsonv2.Unmarshal(scheduleObservations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Items) != 3 {
		t.Fatalf("schedule items = %#v", payload.Items)
	}

	for _, item := range payload.Items {
		if item.IsLive != (item.ChannelID != channelB) || item.ScheduledAt.IsZero() {
			t.Fatalf("schedule item = %#v", item)
		}
	}
}

func TestRunnerPublishesLiveMetadataWithGenerationTwo(t *testing.T) {
	t.Parallel()

	input := holodexInputWithLiveGeneration(
		t,
		"holodex_live",
		[]string{channelA, channelB},
		contract.LiveSnapshotMetadataContractGeneration,
	)

	result, err := NewLiveRunner(&staticFetcher{body: testdata(t, "live.json")}).Collect(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}

	for _, envelope := range result.Output().Observations() {
		if envelope.ObservationKind != contract.KindLiveSnapshot || envelope.SubjectKey != channelA {
			continue
		}

		var payload contract.LiveSnapshotV1

		if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}

		for i := range payload.Sessions {
			if payload.Sessions[i].VideoID != "vidLive01" {
				continue
			}

			if payload.Sessions[i].Title != "Live now" || payload.Sessions[i].TopicID != "minecraft" ||
				payload.Sessions[i].ThumbnailURL != "https://i.ytimg.com/vi/vidLive01/maxresdefault.jpg" {
				t.Fatalf("live metadata = %#v", payload.Sessions[i])
			}

			return
		}
	}

	t.Fatal("generation two live metadata was not published")
}

func TestRunnerPreservesReorderedResponseHash(t *testing.T) {
	t.Parallel()

	body := testdata(t, "live.json")

	var rows []jsontext.Value

	if err := jsonv2.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}

	if rows == nil {
		t.Fatal("live.json must decode into a non-nil array")
	}

	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}

	reversed, err := jsonv2.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseLiveRows(body)
	if err != nil {
		t.Fatal(err)
	}

	parsedReversed, err := parseLiveRows(reversed)
	if err != nil {
		t.Fatal(err)
	}

	input := holodexInput(t, []string{channelA, channelB})
	runner := NewLiveRunner(&staticFetcher{})

	first, err := runner.buildBatch(input, parsed)
	if err != nil {
		t.Fatal(err)
	}

	second, err := runner.buildBatch(input, parsedReversed)
	if err != nil {
		t.Fatal(err)
	}

	firstOutput, err := collectutil.OutputFromEnvelopes(first, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	secondOutput, err := collectutil.OutputFromEnvelopes(second, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if hashes(t, firstOutput) != hashes(t, secondOutput) {
		t.Fatalf("ordering changed hashes\n%s\n%s", hashes(t, firstOutput), hashes(t, secondOutput))
	}
}

func TestRunnerDoesNotPublishOnTimeout(t *testing.T) {
	t.Parallel()

	output, err := NewLiveRunner(&staticFetcher{err: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "timeout")}).Collect(
		t.Context(), holodexInput(t, []string{channelA}),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.Timeout || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerRejectsMalformedSchema(t *testing.T) {
	t.Parallel()

	_, err := NewLiveRunner(&staticFetcher{body: []byte(`{"id":"x"}`)}).Collect(t.Context(), holodexInput(t, []string{channelA}))
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerRejectsConflictingChannelIdentity(t *testing.T) {
	t.Parallel()

	body := []byte(`[{
		"id":"video-a","status":"live","channel_id":"UC_A",
		"channel":{"id":"UC_B"}
	}]`)
	output, err := NewLiveRunner(&staticFetcher{body: body}).Collect(
		t.Context(), holodexInput(t, []string{channelA, channelB}),
	)

	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestMetadataRunnerRejectsConflictingStats(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"video-a","status":"live","channel_id":"UC_A","channel":{"subscriber_count":10,"video_count":2}},
		{"id":"video-b","status":"upcoming","channel_id":"UC_A","channel":{"subscriber_count":11,"video_count":2}}
	]`)
	output, err := NewMetadataRunner(&staticFetcher{body: body}).Collect(
		t.Context(), holodexInputFor(t, "holodex_metadata", []string{channelA}),
	)

	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestMetadataRunnerRejectsConflictingPhotos(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"video-a","status":"live","channel_id":"UC_A","channel":{"photo":"https://img.test/a.jpg"}},
		{"id":"video-b","status":"upcoming","channel_id":"UC_A","channel":{"photo":"https://img.test/b.jpg"}}
	]`)
	output, err := NewMetadataRunner(&staticFetcher{body: body}).Collect(
		t.Context(), holodexInputFor(t, "holodex_metadata", []string{channelA}),
	)

	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerEmitsNothingForEmptyLiveArray(t *testing.T) {
	t.Parallel()

	output := mustCollect(t, testdata(t, "empty.json"), []string{channelA})
	if !output.Empty() {
		t.Fatalf("empty live array published %#v", output.Observations())
	}
}

func TestRunnersKeepCadenceKindsSeparate(t *testing.T) {
	t.Parallel()

	body := testdata(t, "live.json")
	tests := []struct {
		name      string
		runner    *Runner
		jobKind   string
		wantKinds map[contract.ObservationKind]bool
	}{
		{
			name: "live", runner: NewLiveRunner(&staticFetcher{body: body}), jobKind: "holodex_live",
			wantKinds: map[contract.ObservationKind]bool{contract.KindLiveSnapshot: true},
		},
		{
			name: "metadata", runner: NewMetadataRunner(&staticFetcher{body: body}), jobKind: "holodex_metadata",
			wantKinds: map[contract.ObservationKind]bool{contract.KindChannelStats: true, contract.KindChannelPhoto: true},
		},
		{
			name: "schedule", runner: NewScheduleRunner(&staticFetcher{body: body}), jobKind: "holodex_schedule",
			wantKinds: map[contract.ObservationKind]bool{contract.KindSchedule: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := holodexInputFor(t, tt.jobKind, []string{channelA, channelB})

			output, err := tt.runner.Collect(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}

			observations := output.Output().Observations()
			if len(observations) == 0 {
				t.Fatal("runner emitted no observations")
			}

			for _, envelope := range observations {
				if !tt.wantKinds[envelope.ObservationKind] {
					t.Fatalf("%s emitted undeclared kind %s", tt.jobKind, envelope.ObservationKind)
				}
			}
		})
	}
}

func mustCollect(t *testing.T, body []byte, requested []string) collectutil.RunOutput {
	t.Helper()

	output, err := NewLiveRunner(&staticFetcher{body: body}).Collect(t.Context(), holodexInput(t, requested))
	if err != nil {
		t.Fatal(err)
	}

	return output.Output()
}

func hashes(t *testing.T, output collectutil.RunOutput) string {
	t.Helper()

	type pair struct {
		Kind    contract.ObservationKind
		Subject string
		Payload string
		Scope   string
	}

	observations := output.Observations()
	pairs := make([]pair, 0, len(observations))

	for i := range observations {
		envelope := &observations[i]

		pairs = append(pairs, pair{envelope.ObservationKind, envelope.SubjectKey, envelope.PayloadSHA256, envelope.ScopeSHA256})
	}

	slices.SortFunc(pairs, func(left, right pair) int {
		return cmp.Or(
			cmp.Compare(left.Kind, right.Kind),
			cmp.Compare(left.Subject, right.Subject),
		)
	})

	encoded, err := jsonv2.Marshal(pairs)
	if err != nil {
		t.Fatalf("marshal observation hashes: %v", err)
	}

	return string(encoded)
}

func holodexInput(tb testing.TB, requested []string) *collectutil.RunInput {
	tb.Helper()

	return holodexInputFor(tb, "holodex_live", requested)
}

func holodexInputFor(tb testing.TB, jobKind string, requested []string) *collectutil.RunInput {
	tb.Helper()

	return holodexInputWithLiveGeneration(tb, jobKind, requested, 1)
}

func holodexInputWithLiveGeneration(
	tb testing.TB,
	jobKind string,
	requested []string,
	liveGeneration int64,
) *collectutil.RunInput {
	tb.Helper()

	enabled := map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot:   requested,
		contract.KindChannelStats:   requested,
		contract.KindChannelPhoto:   requested,
		contract.KindSchedule:       {officialScheduleSubject},
		contract.KindChannelProfile: nil,
	}
	spec := joblease.JobSpec{
		JobKey: "collector:holodex:" + jobKind + ":global", Provider: contract.ProviderHolodex, Class: "GLOBAL",
		CollectionJobKind: jobKind, SubjectKey: "global:" + jobKind, PollInterval: time.Minute,
	}
	lease := contract.LeaseProof{
		JobKey: "collector:holodex:" + jobKind + ":global", CollectionJobKind: jobKind,
		OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
		ScheduledFor: time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC),
	}
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: contract.ProviderHolodex, Kind: sourceobservation.JobKind(jobKind),
	})
	generations := make(map[contract.ObservationKind]int64, len(job.Emissions()))

	for _, kind := range job.Emissions() {
		generations[kind] = 1
		if kind == contract.KindLiveSnapshot {
			generations[kind] = liveGeneration
		}
	}

	snapshot, err := collectutil.NewContractSnapshot(job.Emissions(), generations)
	if err != nil {
		tb.Fatal(err)
	}

	targets := testutil.TargetSnapshot(tb, dbtest.NewPool(tb), &spec, job, enabled)

	lease.ProjectionGeneration = targets.Generation()

	input, err := collectutil.NewRunInput(&spec, &lease, snapshot, targets, 1, 1<<20, job)
	if err != nil {
		tb.Fatal(err)
	}

	return &input
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

type staticFetcher struct {
	body []byte
	err  error
}

func (f *staticFetcher) Fetch(context.Context) ([]byte, error) {
	return f.body, f.err
}

func TestRunnerDoesNotBuildUnrequestedChannelMetadata(t *testing.T) {
	body := []byte(`[
		{"id":"probe-live","title":"Live","channel_id":"UC_A","status":"live","start_actual":"2026-08-14T10:00:00Z",
		 "channel":{"id":"UC_A","subscriber_count":10,"photo":"https://img.test/first.jpg"}},
		{"id":"probe-soon","title":"Soon","channel_id":"UC_A","status":"upcoming","start_scheduled":"2026-08-14T12:00:00Z",
		 "channel":{"id":"UC_A","subscriber_count":20,"photo":"https://img.test/second.jpg"}}
	]`)

	for _, kind := range []string{"holodex_live", "holodex_schedule", "holodex_metadata"} {
		t.Run(kind, func(t *testing.T) {
			input := holodexInputFor(t, kind, []string{channelA})
			runner := &Runner{client: &staticFetcher{body: body}, jobKind: kind}
			result, err := runner.Collect(t.Context(), input)

			if kind == "holodex_metadata" {
				if collecterr.CodeOf(err) != collecterr.ParserDrift {
					t.Fatalf("conflicting requested metadata error = %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unrequested metadata blocked %s: %v", kind, err)
			}

			observations := result.Output().Observations()
			if len(observations) != 1 {
				t.Fatalf("observations = %d, want one scoped result", len(observations))
			}

			assertMetadataIndependentResult(t, kind, observations[0])
		})
	}
}

func assertLiveScopeWithoutViewers(t *testing.T, envelope contract.Envelope) {
	t.Helper()

	if envelope.ObservationKind != contract.KindLiveSnapshot || envelope.Completeness != contract.CompletenessPartial {
		t.Fatalf("live envelope = %#v", envelope)
	}

	var payload contract.LiveSnapshotV1

	if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Sessions) != 1 {
		t.Fatalf("sessions = %#v", payload.Sessions)
	}

	session := payload.Sessions[0]
	wantStatus := "LIVE"

	if envelope.SubjectKey == channelB {
		wantStatus = "UPCOMING"
	}

	if session.Status != wantStatus || session.ChannelID != envelope.SubjectKey || session.ScheduledAt == nil {
		t.Fatalf("session = %#v", session)
	}
}

func assertMetadataIndependentResult(t *testing.T, kind string, envelope contract.Envelope) {
	t.Helper()

	switch kind {
	case "holodex_live":
		var payload contract.LiveSnapshotV1

		if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}

		if len(payload.Sessions) != 2 || payload.Sessions[0].VideoID != "probe-live" || payload.Sessions[0].Status != "LIVE" ||
			payload.Sessions[1].VideoID != "probe-soon" || payload.Sessions[1].Status != "UPCOMING" {
			t.Fatalf("live sessions = %+v", payload.Sessions)
		}
	case "holodex_schedule":
		var payload contract.ScheduleSnapshotV1

		if err := jsonv2.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}

		if len(payload.Items) != 1 || payload.Items[0].VideoID != "probe-soon" || payload.Items[0].ScheduledAt.Hour() != 12 {
			t.Fatalf("schedule items = %+v", payload.Items)
		}
	default:
		t.Fatalf("unexpected metadata-independent job: %s", kind)
	}
}
