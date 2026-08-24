package officialcollector

import (
	"context"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/testutil"
)

func TestRunnerPublishesCompleteScheduleFixture(t *testing.T) {
	t.Parallel()

	output := mustCollect(t, testdata(t, "success.json"))
	observation := mustSingleObservation(t, output)

	if observation.ObservationKind != contract.KindSchedule {
		t.Fatalf("observation = %#v", observation)
	}

	if observation.Completeness != contract.CompletenessComplete {
		t.Fatalf("completeness = %s", observation.Completeness)
	}

	var payload contract.ScheduleSnapshotV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Items) != 2 {
		t.Fatalf("items = %#v", payload.Items)
	}

	liveCount := 0

	for _, item := range payload.Items {
		if item.IsLive {
			liveCount++
		}
	}

	if liveCount != 1 {
		t.Fatalf("is_live count = %d", liveCount)
	}
}

func TestRunnerTreatsEmptySuccessAsComplete(t *testing.T) {
	t.Parallel()

	output := mustCollect(t, testdata(t, "empty.json"))
	observation := mustSingleObservation(t, output)

	var payload contract.ScheduleSnapshotV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if observation.Completeness != contract.CompletenessComplete || len(payload.Items) != 0 {
		t.Fatalf("empty payload = %#v completeness=%s", payload, observation.Completeness)
	}
}

func TestRunnerKeepsIsLiveAsScheduleEvidenceOnly(t *testing.T) {
	t.Parallel()

	output := mustCollect(t, testdata(t, "success.json"))
	for _, envelope := range output.Observations() {
		if envelope.ObservationKind != contract.KindSchedule {
			t.Fatalf("official adapter emitted %s", envelope.ObservationKind)
		}
	}
}

func TestRunnerRejectsSchemaDrift(t *testing.T) {
	t.Parallel()

	_, err := NewRunner(&staticFetcher{body: []byte(`{"foo":[]}`)}).Collect(t.Context(), officialInput(t))
	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerSkipsInvalidRowsWhenValidRowsRemain(t *testing.T) {
	t.Parallel()

	body := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"not-a-date","url":"https://www.youtube.com/watch?v=invalidrow1","title":"Invalid"},
		{"datetime":"2026/08/14 20:00:00","url":"https://www.youtube.com/watch?v=validrow001","title":"Valid"}
	]}]}`)
	output := mustCollect(t, body)
	observation := mustSingleObservation(t, output)

	var payload contract.ScheduleSnapshotV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Items) != 1 || payload.Items[0].VideoID != "validrow001" {
		t.Fatalf("items = %#v", payload.Items)
	}
}

func TestRunnerRejectsNonEmptyPayloadWhenAllRowsInvalid(t *testing.T) {
	t.Parallel()

	body := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"not-a-date","url":"https://www.youtube.com/watch?v=invalidrow1","title":"Invalid"},
		{"datetime":"2026/08/14 20:00:00","url":"https://example.test/watch?v=invalidrow2","title":"Invalid"}
	]}]}`)
	output, err := NewRunner(&staticFetcher{body: body}).Collect(t.Context(), officialInput(t))

	if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerDoesNotPublishOnTimeout(t *testing.T) {
	t.Parallel()

	output, err := NewRunner(&staticFetcher{err: collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "timeout")}).Collect(t.Context(), officialInput(t))
	if err == nil || collecterr.CodeOf(err) != collecterr.Timeout || !output.IsZero() {
		t.Fatalf("error=%v output=%#v", err, output)
	}
}

func TestRunnerPreservesItemOrderHash(t *testing.T) {
	t.Parallel()

	body := testdata(t, "success.json")
	reversed := []byte(`{"dateGroupList":[{"videoList":[
		{"datetime":"2026/08/14 21:00:00","isLive":true,"url":"https://www.youtube.com/watch?v=lmnopqrstuv","title":"Live","name":"Talent B"},
		{"datetime":"2026/08/14 20:00:00","isLive":false,"url":"https://www.youtube.com/watch?v=abcdefghijk","title":"Talk","name":"Talent A"}
	]}]}`)
	first := mustSingleObservation(t, mustCollect(t, body))
	second := mustSingleObservation(t, mustCollect(t, reversed))

	if first.PayloadSHA256 != second.PayloadSHA256 {
		t.Fatal("ordering changed payload hash")
	}
}

func mustCollect(t *testing.T, body []byte) collectutil.RunOutput {
	t.Helper()

	output, err := NewRunner(&staticFetcher{body: body}).Collect(t.Context(), officialInput(t))
	if err != nil {
		t.Fatal(err)
	}

	return output.Output()
}

func mustSingleObservation(t *testing.T, output collectutil.RunOutput) contract.Envelope {
	t.Helper()

	observations := output.Observations()
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want exactly one", observations)

		return contract.Envelope{}
	}

	return observations[0]
}

func officialInput(tb testing.TB) *collectutil.RunInput {
	tb.Helper()

	spec := joblease.JobSpec{
		JobKey: "collector:hololive_official:official_schedule:global", Provider: contract.ProviderHololiveOfficial,
		Class: "GLOBAL", CollectionJobKind: "official_schedule", SubjectKey: officialScheduleSubject, PollInterval: time.Minute,
	}
	lease := contract.LeaseProof{
		JobKey: "collector:hololive_official:official_schedule:global", CollectionJobKind: "official_schedule",
		OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
		ScheduledFor: time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC),
	}
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: contract.ProviderHololiveOfficial, Kind: "official_schedule",
	})

	snapshot, err := collectutil.NewContractSnapshot(job.Emissions(), map[contract.ObservationKind]int64{contract.KindSchedule: 1})
	if err != nil {
		tb.Fatal(err)
	}

	targets := testutil.TargetSnapshot(tb, &spec, job, map[contract.ObservationKind][]string{
		contract.KindSchedule: {officialScheduleSubject},
	})

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
