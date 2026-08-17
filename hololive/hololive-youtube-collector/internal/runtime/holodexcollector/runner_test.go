package holodexcollector

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"sort"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/testutil"
)

func TestRunnerBuildsOneBatchFromLiveFixture(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "live.json"), []string{"UC_A", "UC_B", "UC_C"})
	observations := output.Observations()
	if len(observations) < 4 {
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

func TestRunnerSameSlotRetryKeepsViewerSampleIdentity(t *testing.T) {
	t.Parallel()
	input := holodexInput(t, []string{"UC_A", "UC_B"})
	runner := NewLiveRunner(&staticFetcher{body: testdata(t, "live.json")})
	first, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	firstOutput := first.Output()
	secondOutput := second.Output()
	firstKey := viewerKey(t, firstOutput, "vidHide03")
	secondKey := viewerKey(t, secondOutput, "vidHide03")
	if firstKey != secondKey {
		t.Fatalf("retry changed observation key %s vs %s", firstKey, secondKey)
	}
	for _, envelope := range firstOutput.Observations() {
		if envelope.ObservationKind != contract.KindViewerSample || envelope.SubjectKey != "vidHide03" {
			continue
		}
		var payload contract.ViewerSampleV1
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.SampleWindowStart.Equal(input.Lease().ScheduledFor) {
			t.Fatalf("sample window = %s, want lease %s", payload.SampleWindowStart, input.Lease().ScheduledFor)
		}
		return
	}
	t.Fatal("hidden viewer sample was not emitted")
}

func TestRunnerKeepsHiddenViewerTyped(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "live.json"), []string{"UC_A", "UC_B"})
	found := false
	for _, envelope := range output.Observations() {
		if envelope.ObservationKind != contract.KindViewerSample || envelope.SubjectKey != "vidHide03" {
			continue
		}
		var payload contract.ViewerSampleV1
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Availability != "HIDDEN" || payload.ViewerCount != nil {
			t.Fatalf("hidden viewer = %#v", payload)
		}
		found = true
	}
	if !found {
		t.Fatal("hidden viewer sample was not emitted")
	}
}

func TestRunnerPreservesReorderedResponseHash(t *testing.T) {
	t.Parallel()
	body := testdata(t, "live.json")
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	reversed, err := json.Marshal(rows)
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
	input := holodexInput(t, []string{"UC_A", "UC_B"})
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
		context.Background(), holodexInput(t, []string{"UC_A"}),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.Timeout || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerRejectsMalformedSchema(t *testing.T) {
	t.Parallel()
	_, err := NewLiveRunner(&staticFetcher{body: []byte(`{"id":"x"}`)}).Collect(context.Background(), holodexInput(t, []string{"UC_A"}))
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
		context.Background(), holodexInput(t, []string{"UC_A", "UC_B"}),
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
		context.Background(), holodexInputFor(t, "holodex_metadata", []string{"UC_A"}),
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
		context.Background(), holodexInputFor(t, "holodex_metadata", []string{"UC_A"}),
	)
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerDoesNotEmitViewerForChannelSubjects(t *testing.T) {
	t.Parallel()
	input := holodexInput(t, []string{"UC_A", "UC_B"})
	input = replaceRoster(t, input, contract.KindViewerSample, []string{"UC_A", "UC_B"})
	output, err := NewLiveRunner(&staticFetcher{body: testdata(t, "live.json")}).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	observations := output.Output().Observations()
	for _, envelope := range observations {
		if envelope.ObservationKind == contract.KindViewerSample {
			t.Fatalf("emitted viewer_sample %q from channel roster", envelope.SubjectKey)
		}
	}
	if len(observations) == 0 {
		t.Fatal("channel-kind observations were dropped with viewers")
	}
}

func TestRunnerEmitsNothingForEmptyLiveArray(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "empty.json"), []string{"UC_A"})
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
			wantKinds: map[contract.ObservationKind]bool{contract.KindLiveSnapshot: true, contract.KindViewerSample: true},
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
			input := holodexInputFor(t, tt.jobKind, []string{"UC_A", "UC_B"})
			output, err := tt.runner.Collect(context.Background(), input)
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
	output, err := NewLiveRunner(&staticFetcher{body: body}).Collect(context.Background(), holodexInput(t, requested))
	if err != nil {
		t.Fatal(err)
	}
	return output.Output()
}

func viewerKey(t *testing.T, output collectutil.RunOutput, subject string) string {
	t.Helper()
	observations := output.Observations()
	for i := range observations {
		envelope := &observations[i]
		if envelope.ObservationKind == contract.KindViewerSample && envelope.SubjectKey == subject {
			return envelope.ObservationKey
		}
	}
	t.Fatalf("viewer sample %s was not emitted", subject)
	return ""
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
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Kind != pairs[j].Kind {
			return pairs[i].Kind < pairs[j].Kind
		}
		return pairs[i].Subject < pairs[j].Subject
	})
	encoded, err := json.Marshal(pairs)
	if err != nil {
		t.Fatalf("marshal observation hashes: %v", err)
	}
	return string(encoded)
}

func holodexInput(t testing.TB, requested []string) *collectutil.RunInput {
	t.Helper()
	return holodexInputFor(t, "holodex_live", requested)
}

func holodexInputFor(t testing.TB, jobKind string, requested []string) *collectutil.RunInput {
	t.Helper()
	enabled := map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot:   requested,
		contract.KindChannelStats:   requested,
		contract.KindChannelPhoto:   requested,
		contract.KindViewerSample:   {"vidLive01", "vidSoon02", "vidHide03"},
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
		ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: contract.ProviderHolodex, Kind: sourceobservation.JobKind(jobKind),
	})
	generations := make(map[contract.ObservationKind]int64, len(job.Emissions()))
	for _, kind := range job.Emissions() {
		generations[kind] = 1
	}
	snapshot, err := collectutil.NewContractSnapshot(job.Emissions(), generations)
	if err != nil {
		t.Fatal(err)
	}
	targets := testutil.TargetSnapshot(t, &spec, job, enabled)
	lease.ProjectionGeneration = targets.Generation()
	input, err := collectutil.NewRunInput(&spec, &lease, snapshot, targets, 1, 1<<20, job)
	if err != nil {
		t.Fatal(err)
	}
	return &input
}

func replaceRoster(t testing.TB, input *collectutil.RunInput, kind contract.ObservationKind, subjects []string) *collectutil.RunInput {
	t.Helper()
	job := input.Job()
	enabled := make(map[contract.ObservationKind][]string)
	for _, requested := range job.RequestedKinds() {
		if requested == kind {
			enabled[requested] = subjects
			continue
		}
		subjectsForKind, err := input.Roster(requested)
		if err != nil {
			t.Fatal(err)
		}
		enabled[requested] = subjectsForKind
	}
	generations := make(map[contract.ObservationKind]int64, len(job.Emissions()))
	for _, emitted := range job.Emissions() {
		generation, err := input.Generation(emitted)
		if err != nil {
			t.Fatal(err)
		}
		generations[emitted] = generation
	}
	snapshot, err := collectutil.NewContractSnapshot(job.Emissions(), generations)
	if err != nil {
		t.Fatal(err)
	}
	inputSpec := input.Spec()
	targets := testutil.TargetSnapshot(t, &inputSpec, job, enabled)
	lease := input.Lease()
	lease.ProjectionGeneration = targets.Generation()
	result, err := collectutil.NewRunInput(
		&inputSpec, &lease, snapshot, targets,
		input.MaxPages(), input.MaxSuccessResponseBytes(), job,
	)
	if err != nil {
		t.Fatal(err)
	}
	return &result
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
