package sourceobservation

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestLiveEndBeforePositiveSurvivesPersist(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	liveResult, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &proof, liveSession(testVideoID, testStatusLive))))
	if err != nil {
		t.Fatal(err)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	endResult, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &proof, liveSession(testVideoID, testStatusEnded))))
	if err != nil {
		t.Fatal(err)
	}

	batch, err := repo.ClaimBatch(ctx, liveClaimOptions())
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{endResult.Results[0].ObservationID, liveResult.Results[0].ObservationID} {
		consumeLiveClaim(ctx, t, consumer, batch, id)
	}

	var processed int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_queue WHERE status = 'PROCESSED'`).Scan(&processed); err != nil {
		t.Fatal(err)
	}

	t.Logf("processed observations=%d", processed)

	if got := liveSessionStatus(t, pool); got != testStatusEnded {
		t.Fatalf("persisted status=%s, want ENDED after consuming the same LIVE/ENDED evidence in reverse order", got)
	}
}

func TestLiveEvidenceUpgradeRestoresExistingCandidate(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))

	endedAt := proof.ScheduledFor.Add(-15 * time.Second)
	fact := liveSession(testVideoID, testStatusEnded)

	fact.EndedAt = &endedAt
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, fact)

	// helper가 소유한 DB에서만 이전 저장 형태로 되돌려 production migration을 재생한다.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE source_observations ADD COLUMN payload JSONB;
		UPDATE source_observations observation SET payload = dictionary.payload
		FROM source_observation_payloads dictionary WHERE dictionary.id = observation.payload_id;
		ALTER TABLE youtube_live_reconciliation_heads DROP CONSTRAINT fk_youtube_live_head_pending_end;
		DROP TABLE youtube_live_pending_ends, youtube_live_absence_slots;
		ALTER TABLE youtube_live_reconciliation_heads
			DROP COLUMN first_absence_scheduled_for, DROP COLUMN second_absence_scheduled_for,
			DROP COLUMN last_absence_observation_id, DROP COLUMN ignored_absence_scheduled_for,
			ADD CONSTRAINT youtube_live_reconciliation_h_end_candidate_observation_id_fkey
			FOREIGN KEY (end_candidate_observation_id) REFERENCES source_observations(id) ON DELETE RESTRICT;
		DELETE FROM hololive_dbtest_internal.schema_migrations
		WHERE filename IN ('192_live_reconciliation_evidence.sql', '193_live_absence_slot_channel_index.sql');
	`); err != nil {
		t.Fatal(err)
	}

	if err := dbtest.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}

	if err := dbtest.ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("idempotent migration replay: %v", err)
	}

	if _, err := pool.Exec(ctx, `ALTER TABLE source_observations DROP COLUMN payload`); err != nil {
		t.Fatal(err)
	}

	var restored time.Time

	if err := pool.QueryRow(ctx, "SELECT ended_at FROM youtube_live_pending_ends WHERE video_id=$1", testVideoID).Scan(&restored); err != nil {
		t.Fatal(err)
	}

	if !restored.Equal(endedAt) {
		t.Fatalf("restored ended_at=%s want %s", restored, endedAt)
	}

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=NOW()-INTERVAL '2 hours', next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("upgraded finalizer processed=%t err=%v", processed, err)
	}

	if got := liveSessionStatus(t, pool); got != testStatusEnded {
		t.Fatalf("status=%s", got)
	}
}

func TestLiveSteadyConsumeAndFinalizerDoNotReloadOldAbsenceHistory(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	// 오래된 slot 10,000개의 필터 decode 오류가 현재 session과 확정 candidate에 전파되면 안 된다.
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_absence_slots
		SELECT 1000000+g, $1::timestamptz-g*INTERVAL '1 minute', repeat('a',64),
		       $1::timestamptz-g*INTERVAL '1 minute', $1, repeat('b',64),
		       jsonb_build_object('requested_channel_ids',jsonb_build_array($2::text),'filters','invalid')
		FROM generate_series(1,10000) AS g
	`, proof.ScheduledFor.Add(-time.Hour), testChannelID); err != nil {
		t.Fatal(err)
	}

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	// 신규 LIVE도 positive 이전의 이력을 디코딩할 필요가 없다.
	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession("new-video", testStatusLive))
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=NOW()-INTERVAL '2 hours', next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("long-history finalizer processed=%t err=%v", processed, err)
	}
}

func TestLiveGracePreservesExplicitEndedAt(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))

	endedAt := proof.ScheduledFor.Add(-15 * time.Second)
	fact := liveSession(testVideoID, testStatusEnded)

	fact.EndedAt = &endedAt
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, fact)

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=NOW()-INTERVAL '2 hours', next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("processed=%t err=%v", processed, err)
	}

	var got time.Time

	if err := pool.QueryRow(ctx, `SELECT ended_at FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&got); err != nil {
		t.Fatal(err)
	}

	if !got.Equal(endedAt) {
		t.Fatalf("ended_at=%s, want provider explicit ended_at=%s", got, endedAt)
	}
}

func TestLiveEvidenceDBPermutationsConverge(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []string
		orders   [][]int
	}{
		{name: "live-ended", statuses: []string{testStatusLive, testStatusEnded}, orders: [][]int{{0, 1}, {1, 0}}},
		{name: "upcoming-canceled", statuses: []string{testStatusUpcoming, testStatusCanceled}, orders: [][]int{{0, 1}, {1, 0}}},
		{name: "live-two-absences", statuses: []string{testStatusLive, "", ""}, orders: [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}},
	} {
		provider, jobKind, generation := contract.ProviderYouTubeJS, "youtubejs_channel_live", contract.LiveSnapshotQueryContractGeneration

		if test.name == "upcoming-canceled" {
			provider, jobKind, generation = contract.ProviderHolodex, "holodex_live", contract.LiveSnapshotMetadataContractGeneration
		}

		for _, order := range test.orders {
			t.Run(fmt.Sprint(test.name, order), func(t *testing.T) {
				ctx := t.Context()
				pool := dbtest.NewPool(t)
				repo := NewRepository(pool)
				consumer := NewConsumerWithGraces(repo, 0, 0)
				proof := seedPublishLease(ctx, t, pool, provider, contract.KindLiveSnapshot, testChannelID, jobKind)
				ids := make([]int64, 0, len(test.statuses))

				for _, status := range test.statuses {
					var facts []contract.LiveSessionV1

					if status != "" {
						facts = append(facts, liveSession(testVideoID, status))
					}

					envelope := liveSnapshotEnvelopeFromProviderAtGeneration(t, &proof, generation, provider, testChannelID, facts...)

					result, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(envelope))
					if err != nil {
						t.Fatal(err)
					}

					ids = append(ids, result.Results[0].ObservationID)
					proof = advanceLease(ctx, t, pool, &proof, time.Minute)
				}

				batch, err := repo.ClaimBatch(ctx, liveClaimOptions())
				if err != nil {
					t.Fatal(err)
				}

				for _, index := range order {
					consumeLiveClaim(ctx, t, consumer, batch, ids[index])
				}

				if got := liveSessionStatus(t, pool); got != testStatusEnded {
					t.Fatalf("status=%s, want ENDED", got)
				}

				assertTableCount(t, pool, "youtube_live_pending_ends", 0)
			})
		}
	}
}

func TestLiveSameSlotKeepsFirstCoverageAcrossProviders(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))

	holodexProof := proof

	holodexProof.JobKey = "job:holodex_live:slot-coverage"
	holodexProof.CollectionJobKind = "holodex_live"

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at,
			membership_kinds, membership_exact_subject, membership_target_count
		) VALUES ($1, 'holodex', 'GLOBAL', 'holodex_live', $2, $3, 60000, 'ACTIVE', $4, $4, $5, $6, NOW()+INTERVAL '1 hour',
		          ARRAY['live_snapshot']::text[], FALSE, 1)
	`, holodexProof.JobKey, testChannelID, proof.ProjectionGeneration, proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatal(err)
	}

	if !holodexProof.ScheduledFor.Equal(proof.ScheduledFor) {
		t.Fatal("fixture must reuse the same slot")
	}

	// Holodex의 첫 coverage는 UPCOMING만 포함한다. 같은 slot의 YouTube 전체 조회가 이를 바꾸지 못한다.
	publishConsumeLiveFromProvider(ctx, t, publishkit.NewPublisher(pool), consumer, &holodexProof, contract.ProviderHolodex, testChannelID, liveSession("other-upcoming", testStatusUpcoming))
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)

	if slots := liveConsecutiveSlots(t, pool, testVideoID); slots != 0 {
		t.Fatalf("same slot replaced its first coverage: count=%d", slots)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)

	if slots := liveConsecutiveSlots(t, pool, testVideoID); slots != 1 {
		t.Fatalf("distinct slot count=%d, want one", slots)
	}

	if got := liveSessionStatus(t, pool); got != testStatusLive {
		t.Fatalf("status=%s, want LIVE after only one absence", got)
	}
}

func TestLiveEndEvidenceSurvivesRawRetentionAndGrace(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	consumer := NewConsumerWithGraces(repo, 0, time.Hour)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderHolodex, contract.KindLiveSnapshot, testChannelID, "holodex_live")
	// Holodex의 명시적 종료 근거로 원본 삭제 후 grace를 검사한다. streams 전체 coverage의 부재 근거는 섞지 않는다.
	positiveFact := liveSession(testVideoID, testStatusLive)

	positiveFact.StartedAt = new(proof.ScheduledFor)

	positive, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(liveSnapshotEnvelopeFromProviderAtGeneration(t, &proof, contract.LiveSnapshotMetadataContractGeneration, contract.ProviderHolodex, testChannelID, positiveFact)))
	if err != nil {
		t.Fatal(err)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	endedAt := proof.ScheduledFor.Add(-15 * time.Second)
	fact := liveSession(testVideoID, testStatusEnded)

	fact.EndedAt = &endedAt

	negative, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(liveSnapshotEnvelopeFromProviderAtGeneration(t, &proof, contract.LiveSnapshotMetadataContractGeneration, contract.ProviderHolodex, testChannelID, fact)))
	if err != nil {
		t.Fatal(err)
	}

	batch, err := repo.ClaimBatch(ctx, liveClaimOptions())
	if err != nil {
		t.Fatal(err)
	}

	consumeLiveClaim(ctx, t, consumer, batch, negative.Results[0].ObservationID)

	if _, err = pool.Exec(ctx, "DELETE FROM source_observation_queue WHERE observation_id=$1", negative.Results[0].ObservationID); err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.deleteEvidenceBatch(ctx, RetentionConfig{EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{contract.KindLiveSnapshot: time.Hour}, BatchSize: 10}, time.Now().Add(2*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("retention deleted=%d err=%v", deleted, err)
	}

	consumeLiveClaim(ctx, t, consumer, batch, positive.Results[0].ObservationID)

	if _, err = pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=NOW()-INTERVAL '2 hours', next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("processed=%t err=%v", processed, err)
	}

	var got time.Time

	if err := pool.QueryRow(ctx, "SELECT ended_at FROM youtube_live_sessions WHERE video_id=$1", testVideoID).Scan(&got); err != nil {
		t.Fatal(err)
	}

	if !got.Equal(endedAt) {
		t.Fatalf("ended_at=%s, want %s", got, endedAt)
	}

	assertTableCount(t, pool, "youtube_live_pending_ends", 0)
}

func TestLiveFinalizerUsesConsumerLockOrderAndRechecksDue(t *testing.T) {
	pool, _, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	defer cancel()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	consumerTx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(ctx, t, consumerTx, "live consumer")

	_, err = consumerTx.Exec(ctx, mustSQL("repository_live_sessions_0045_45.sql"), []string{testChannelID}, []string{testVideoID})
	require.NoError(t, err)

	finalizerTx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(ctx, t, finalizerTx, "live finalizer")

	pid := finalizerTx.Conn().PgConn().PID()
	done := make(chan error, 1)

	var processed bool

	go func() {
		var finalizeErr error

		processed, finalizeErr = finalizeNextDueLiveEndTx(ctx, finalizerTx, time.Hour)

		done <- finalizeErr
	}()

	ticker := time.NewTicker(5 * time.Millisecond)

	defer ticker.Stop()

	for {
		var waiting bool

		require.NoError(t, pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&waiting))

		if waiting {
			break
		}

		select {
		case finalizeErr := <-done:
			t.Fatalf("finalizer did not wait for session: %v", finalizeErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}

	// finalizer가 session을 기다리는 동안 consumer가 head를 갱신할 수 있어야 한다.
	_, err = consumerTx.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET next_end_check_at=NOW()+INTERVAL '1 hour' WHERE video_id=$1`, testVideoID)
	require.NoError(t, err)

	require.NoError(t, consumerTx.Commit(ctx))
	require.NoError(t, <-done)
	require.False(t, processed, "finalizer used the stale due selection after acquiring locks")
	require.NoError(t, finalizerTx.Commit(ctx))
}

func consumeLiveClaim(ctx context.Context, t *testing.T, consumer *Consumer, batch ClaimedBatch, observationID int64) {
	t.Helper()

	index := slices.IndexFunc(batch.Claims, func(work ClaimWork) bool { return work.ObservationID == observationID })
	require.GreaterOrEqual(t, index, 0, "missing claim for observation %d", observationID)

	require.NoError(t, consumer.ConsumeClaim(ctx, batch.Claims[index].Claim(batch.ConsumerName)))
}
