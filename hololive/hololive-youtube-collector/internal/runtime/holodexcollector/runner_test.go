package holodexcollector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestRunnerBuildsOneBatchFromLiveFixture(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "live.json"), []string{"UC_A", "UC_B", "UC_C"})
	if len(output.Observations) < 4 {
		t.Fatalf("observations = %d", len(output.Observations))
	}
	kinds := map[contract.ObservationKind]int{}
	subjects := map[string]struct{}{}
	for _, envelope := range output.Observations {
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
	input := holodexInput([]string{"UC_A", "UC_B"})
	runner := NewRunner(&staticFetcher{body: testdata(t, "live.json")})
	first, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	firstKey := viewerKey(t, first, "vidHide03")
	secondKey := viewerKey(t, second, "vidHide03")
	if firstKey != secondKey {
		t.Fatalf("retry changed observation key %s vs %s", firstKey, secondKey)
	}
	for _, envelope := range first.Observations {
		if envelope.ObservationKind != contract.KindViewerSample || envelope.SubjectKey != "vidHide03" {
			continue
		}
		var payload contract.ViewerSampleV1
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.SampleWindowStart.Equal(input.Lease.ScheduledFor) {
			t.Fatalf("sample window = %s, want lease %s", payload.SampleWindowStart, input.Lease.ScheduledFor)
		}
		return
	}
	t.Fatal("hidden viewer sample was not emitted")
}

func TestRunnerKeepsHiddenViewerTyped(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "live.json"), []string{"UC_A", "UC_B"})
	found := false
	for _, envelope := range output.Observations {
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
	input := holodexInput([]string{"UC_A", "UC_B"})
	runner := NewRunner(&staticFetcher{})
	first, err := runner.buildBatch(input, parsed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.buildBatch(input, parsedReversed)
	if err != nil {
		t.Fatal(err)
	}
	if hashes(collectutil.RunOutput{Observations: first}) != hashes(collectutil.RunOutput{Observations: second}) {
		t.Fatalf("ordering changed hashes\n%s\n%s", hashes(collectutil.RunOutput{Observations: first}), hashes(collectutil.RunOutput{Observations: second}))
	}
}

func TestRunnerDoesNotPublishOnTimeout(t *testing.T) {
	t.Parallel()
	output, err := NewRunner(&staticFetcher{err: collecterr.New(collecterr.Timeout, "timeout")}).Collect(
		context.Background(), holodexInput([]string{"UC_A"}),
	)
	if err == nil || collecterr.Code(err) != collecterr.Timeout || len(output.Observations) != 0 {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerRejectsMalformedSchema(t *testing.T) {
	t.Parallel()
	_, err := NewRunner(&staticFetcher{body: []byte(`{"id":"x"}`)}).Collect(context.Background(), holodexInput([]string{"UC_A"}))
	if err == nil || collecterr.Code(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerDoesNotEmitViewerForChannelSubjects(t *testing.T) {
	t.Parallel()
	input := holodexInput([]string{"UC_A", "UC_B"})
	input.EnabledSubjects[contract.KindViewerSample] = []string{"UC_A", "UC_B"}
	output, err := NewRunner(&staticFetcher{body: testdata(t, "live.json")}).Collect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, envelope := range output.Observations {
		if envelope.ObservationKind == contract.KindViewerSample {
			t.Fatalf("emitted viewer_sample %q from channel roster", envelope.SubjectKey)
		}
	}
	if len(output.Observations) == 0 {
		t.Fatal("channel-kind observations were dropped with viewers")
	}
}

func TestRunnerEmitsNothingForEmptyLiveArray(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "empty.json"), []string{"UC_A"})
	if len(output.Observations) != 0 {
		t.Fatalf("empty live array published %#v", output.Observations)
	}
}

func mustCollect(t *testing.T, body []byte, requested []string) collectutil.RunOutput {
	t.Helper()
	output, err := NewRunner(&staticFetcher{body: body}).Collect(context.Background(), holodexInput(requested))
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func viewerKey(t *testing.T, output collectutil.RunOutput, subject string) string {
	t.Helper()
	for _, envelope := range output.Observations {
		if envelope.ObservationKind == contract.KindViewerSample && envelope.SubjectKey == subject {
			return envelope.ObservationKey
		}
	}
	t.Fatalf("viewer sample %s was not emitted", subject)
	return ""
}

func hashes(output collectutil.RunOutput) string {
	type pair struct {
		Kind    contract.ObservationKind
		Subject string
		Payload string
		Scope   string
	}
	pairs := make([]pair, 0, len(output.Observations))
	for _, envelope := range output.Observations {
		pairs = append(pairs, pair{envelope.ObservationKind, envelope.SubjectKey, envelope.PayloadSHA256, envelope.ScopeSHA256})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Kind != pairs[j].Kind {
			return pairs[i].Kind < pairs[j].Kind
		}
		return pairs[i].Subject < pairs[j].Subject
	})
	encoded, _ := json.Marshal(pairs)
	return string(encoded)
}

func holodexInput(requested []string) collectutil.RunInput {
	enabled := map[contract.ObservationKind][]string{
		contract.KindLiveSnapshot:   requested,
		contract.KindChannelStats:   requested,
		contract.KindChannelPhoto:   requested,
		contract.KindViewerSample:   {"vidLive01", "vidSoon02", "vidHide03"},
		contract.KindSchedule:       {officialScheduleSubject},
		contract.KindChannelProfile: nil,
	}
	return collectutil.RunInput{
		Spec: joblease.JobSpec{
			JobKey: "collector:holodex:global", Provider: contract.ProviderHolodex, Class: "GLOBAL",
			CollectionJobKind: "holodex_global", SubjectKey: "global:holodex_global", PollInterval: time.Minute,
		},
		Lease: contract.LeaseProof{
			JobKey: "collector:holodex:global", CollectionJobKind: "holodex_global",
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
			ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
		},
		ContractGenerations: map[contract.ObservationKind]int64{
			contract.KindLiveSnapshot: 1, contract.KindViewerSample: 1, contract.KindChannelStats: 1,
			contract.KindChannelProfile: 1, contract.KindChannelPhoto: 1, contract.KindSchedule: 1,
		},
		RequestedChannelIDs: requested,
		EnabledSubjects:     enabled,
	}
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
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
