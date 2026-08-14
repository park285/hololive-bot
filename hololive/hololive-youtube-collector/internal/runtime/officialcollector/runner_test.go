package officialcollector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestRunnerPublishesCompleteScheduleFixture(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "success.json"))
	if len(output.Observations) != 1 || output.Observations[0].ObservationKind != contract.KindSchedule {
		t.Fatalf("observations = %#v", output.Observations)
	}
	if output.Observations[0].Completeness != contract.CompletenessComplete {
		t.Fatalf("completeness = %s", output.Observations[0].Completeness)
	}
	var payload contract.ScheduleSnapshotV1
	if err := json.Unmarshal(output.Observations[0].Payload, &payload); err != nil {
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
	var payload contract.ScheduleSnapshotV1
	if err := json.Unmarshal(output.Observations[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if output.Observations[0].Completeness != contract.CompletenessComplete || len(payload.Items) != 0 {
		t.Fatalf("empty payload = %#v completeness=%s", payload, output.Observations[0].Completeness)
	}
}

func TestRunnerKeepsIsLiveAsScheduleEvidenceOnly(t *testing.T) {
	t.Parallel()
	output := mustCollect(t, testdata(t, "success.json"))
	for _, envelope := range output.Observations {
		if envelope.ObservationKind != contract.KindSchedule {
			t.Fatalf("official adapter emitted %s", envelope.ObservationKind)
		}
	}
}

func TestRunnerRejectsSchemaDrift(t *testing.T) {
	t.Parallel()
	_, err := NewRunner(&staticFetcher{body: []byte(`{"foo":[]}`)}).Collect(context.Background(), officialInput())
	if err == nil || collecterr.Code(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerDoesNotPublishOnTimeout(t *testing.T) {
	t.Parallel()
	output, err := NewRunner(&staticFetcher{err: collecterr.New(collecterr.Timeout, "timeout")}).Collect(context.Background(), officialInput())
	if err == nil || collecterr.Code(err) != collecterr.Timeout || len(output.Observations) != 0 {
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
	first := mustCollect(t, body).Observations[0]
	second := mustCollect(t, reversed).Observations[0]
	if first.PayloadSHA256 != second.PayloadSHA256 {
		t.Fatalf("ordering changed payload hash")
	}
}

func mustCollect(t *testing.T, body []byte) collectutil.RunOutput {
	t.Helper()
	output, err := NewRunner(&staticFetcher{body: body}).Collect(context.Background(), officialInput())
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func officialInput() collectutil.RunInput {
	return collectutil.RunInput{
		Spec: joblease.JobSpec{
			JobKey: "collector:hololive_official:global", Provider: contract.ProviderHololiveOfficial,
			Class: "GLOBAL", CollectionJobKind: "official_schedule", SubjectKey: officialScheduleSubject, PollInterval: time.Minute,
		},
		Lease: contract.LeaseProof{
			JobKey: "collector:hololive_official:global", CollectionJobKind: "official_schedule",
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
			ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
		},
		ContractGenerations: map[contract.ObservationKind]int64{contract.KindSchedule: 1},
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
