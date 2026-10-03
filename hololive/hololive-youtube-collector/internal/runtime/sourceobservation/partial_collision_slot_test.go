package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestRepeatedPartialCollisionAdvancesSlotAndKeepsFreshVideos(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	leases := partialSlotLeaseRepository(t, pool)
	repo := NewRepository(pool)
	spec := joblease.JobSpec{
		JobKey: proof.JobKey, Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "youtubejs_content", SubjectKey: testChannelID, PollInterval: time.Second,
	}
	deferInput := mustTestDeferInput(t, contract.ErrorParserDrift, contract.ClassDataContract, "shorts parser drift")
	attempts := []struct {
		video   string
		outcome PublishOutcome
	}{
		{"video-first", PublishInserted},
		{"video-first", PublishDuplicate},
		{"video-second", PublishCollision},
		{"video-second", PublishInserted},
		{"video-third", PublishCollision},
		{"video-third", PublishInserted},
		{"video-fourth", PublishCollision},
		{"video-fourth", PublishInserted},
	}

	var previousTerminal leaseTerminalState

	for i, attempt := range attempts {
		if i > 0 {
			// 정상 cadence와 bounded defer가 실제 DB 시계에서 due가 된 뒤 재획득합니다.
			time.Sleep(1100 * time.Millisecond)

			lease, err := leases.Acquire(ctx, &spec, proof.OwnerInstance)
			if err != nil {
				t.Fatal(err)
			}

			previousSlot := proof.ScheduledFor

			proof = lease.Proof()

			if previousTerminal.state == testSlotStateIdle && !proof.ScheduledFor.After(previousSlot) {
				t.Fatalf("collision did not advance scheduled slot: %s -> %s", previousSlot, proof.ScheduledFor)
			}

			if previousTerminal.state == "DEFERRED" && !proof.ScheduledFor.Equal(previousSlot) {
				t.Fatalf("non-collision partial retry changed identity: %s -> %s", previousSlot, proof.ScheduledFor)
			}
		}

		envelope := partialSlotVideoEnvelope(t, &proof, attempt.video)

		result, err := repo.PublishBatchAndDefer(ctx, publishInput(&envelope), deferInput)
		if err != nil {
			t.Fatal(err)
		}

		if got := result.Results[0].Outcome; got != attempt.outcome {
			t.Fatalf("partial attempt %d outcome = %s, want %s", i+1, got, attempt.outcome)
		}

		previousTerminal = readLeaseTerminal(ctx, t, pool, proof.JobKey)
		assertPartialSlotTerminal(t, &previousTerminal, attempt.outcome)
	}

	assertTableCount(t, pool, "source_observations", 4)
	assertTableCount(t, pool, "source_observation_queue", 4)
	assertTableCount(t, pool, "source_observation_collisions", 3)
}

func partialSlotLeaseRepository(t *testing.T, pool *pgxpool.Pool) *joblease.Repository {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_targets SET poll_interval_ms = 1000`); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_job_leases SET poll_interval_ms = 1000`); err != nil {
		t.Fatal(err)
	}

	config := joblease.Config{
		LeaseTTL: time.Minute, RenewInterval: 20 * time.Second, RenewTimeout: 5 * time.Second,
		DBTimeout: time.Second, CleanupTimeout: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 10, QueueCapacity: 4,
	}

	repo, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func partialSlotVideoEnvelope(t *testing.T, proof *contract.LeaseProof, videoID string) contract.Envelope {
	t.Helper()

	payload := contract.VideoListV1{
		ChannelID: testChannelID,
		Videos: []contract.VideoListItemV1{{
			VideoID: videoID, ChannelID: testChannelID, Title: videoID,
		}},
		Coverage: contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true},
	}

	envelope, err := collection.Envelope(
		contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, contract.VideoListPublicationContractGeneration, proof,
		contract.CompletenessComplete, contract.ContinuityContiguous, payload,
	)
	if err != nil {
		t.Fatal(err)
	}

	return envelope
}

func assertPartialSlotTerminal(t *testing.T, terminal *leaseTerminalState, outcome PublishOutcome) {
	t.Helper()

	if outcome == PublishCollision {
		if terminal.state != testSlotStateIdle || terminal.retryAt != nil ||
			terminal.errorCode != string(contract.ErrorObservationCollision) ||
			terminal.failureCode != string(contract.ErrorObservationCollision) {
			t.Fatalf("known collision did not complete with collision diagnostic: %#v", terminal)
		}

		return
	}

	if terminal.state != "DEFERRED" || terminal.retryAt == nil || terminal.failureCode != string(contract.ErrorParserDrift) {
		t.Fatalf("non-collision partial did not preserve defer diagnostic: %#v", terminal)
	}
}
