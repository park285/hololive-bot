package sourceobservation

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func startUnobservedVideoLifecycle(t *testing.T, origin string) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	pool, repo, consumer, proof := startLivePersist(t)

	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_sessions
        (video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
        VALUES ($1,$2,'UPCOMING',NULL,now()-interval '1 year',$3)`, testVideoID, testChannelID, origin)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), `UPDATE observation_contract_generations SET current_schema_version=2,current_generation=2
        WHERE provider='youtubejs' AND observation_kind='video_live_check'`)
	if err != nil {
		t.Fatal(err)
	}

	proof = seedAdditionalLease(t, pool, &proof, contract.ProviderYouTubeJS, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

	return pool, repo, consumer, proof
}

func lifecycleVideoEnvelope(t *testing.T, proof *contract.LeaseProof, payload contract.VideoLiveCheckV1) *contract.Envelope {
	t.Helper()

	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		t.Fatal(err)
	}

	completeness := contract.CompletenessPartial

	if payload.Availability == contract.VideoAvailabilityUnknown {
		completeness = contract.CompletenessUnknown
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindVideoLiveCheck, SubjectKey: testVideoID,
		SchemaVersion: contract.VideoLifecycleSchemaVersion, ContractGeneration: contract.VideoLifecycleContractGeneration,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: completeness, Continuity: contract.ContinuityNotApplicable,
		Payload: raw, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &envelope
}

func TestVideoLifecycleSettlesUnobservedEndWithoutStartOrNotification(t *testing.T) {
	for _, origin := range []string{"metadata_only", "legacy_unknown"} {
		t.Run(origin, func(t *testing.T) {
			pool, repo, consumer, proof := startUnobservedVideoLifecycle(t, origin)
			ended := proof.ScheduledFor.Add(-time.Minute)
			id := publishLiveCheck(t.Context(), t, repo, lifecycleVideoEnvelope(t, &proof, endedVideoCheck(ended)))
			consumeLiveChecks(t.Context(), t, consumer)

			var (
				status, headStatus, gotOrigin                      string
				gotEnd                                             time.Time
				started, firstSeen, upcomingPositive, livePositive *time.Time
			)

			if err := pool.QueryRow(t.Context(), `SELECT session.status,head.status,session.lifecycle_origin,session.ended_at,
                session.started_at,session.live_first_seen_at,head.last_upcoming_positive_at,head.last_live_positive_at
                FROM youtube_live_sessions session JOIN youtube_live_reconciliation_heads head USING(video_id)
                WHERE video_id=$1`, testVideoID).Scan(&status, &headStatus, &gotOrigin, &gotEnd, &started, &firstSeen, &upcomingPositive, &livePositive); err != nil {
				t.Fatal(err)
			}

			if status != "ENDED" || headStatus != "ENDED" || gotOrigin != "observed" || !gotEnd.Equal(ended) ||
				started != nil || firstSeen != nil || upcomingPositive != nil || livePositive != nil {
				t.Fatalf("terminal reconciliation fabricated start: %s/%s/%s/%v/%v/%v/%v/%v", status, headStatus, gotOrigin, gotEnd, started, firstSeen, upcomingPositive, livePositive)
			}

			assertApplicationDecision(t, pool, id, liveSessionEntityKind, "ENDED")
			assertTableCount(t, pool, "youtube_notification_outbox", 0)
			assertTableCount(t, pool, "youtube_live_pending_ends", 0)
		})
	}
}

func TestVideoLifecycleWaitingRequiresNewScheduleProof(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "no schedule proof", true: "new waiting proof"}[confirmed], func(t *testing.T) {
			pool, repo, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
			payload := endedVideoCheck(proof.ScheduledFor)

			payload.EndedAt = nil
			payload.IsUpcoming = new(true)

			if confirmed {
				payload.WaitingStateConfirmed = new(true)
				payload.ScheduledAt = new(proof.ScheduledFor.Add(time.Hour))
			}

			publishLiveCheck(t.Context(), t, repo, lifecycleVideoEnvelope(t, &proof, payload))
			consumeLiveChecks(t.Context(), t, consumer)

			if confirmed {
				assertLifecycleOrigin(t, pool, "observed")
				assertTableCount(t, pool, "youtube_live_reconciliation_heads", 1)
			} else {
				assertLifecycleOrigin(t, pool, "legacy_unknown")
				assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
			}

			var reviewable bool

			if err := pool.QueryRow(t.Context(), `SELECT reviewable FROM youtube_live_review_snapshot($1)`, testVideoID).Scan(&reviewable); err != nil {
				t.Fatal(err)
			}

			if reviewable == confirmed {
				t.Fatal("availability was confused with lifecycle uncertainty during review")
			}
		})
	}
}

func TestVideoLifecycleUnobservedSessionRejectsAmbiguousOrOlderFacts(t *testing.T) {
	for _, scenario := range []string{"channel mismatch", "private only", "missing end", "newer positive", "newer pending"} {
		t.Run(scenario, func(t *testing.T) {
			pool, repo, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
			payload := endedVideoCheck(proof.ScheduledFor.Add(-time.Minute))

			switch scenario {
			case "channel mismatch":
				payload.ChannelID = "UC_OTHER"
			case "private only":
				payload.EndedAt = nil
				payload.IsPrivate = new(true)
				payload.Availability = contract.VideoAvailabilityPublicUnavailable
				payload.Method = contract.VideoAvailabilityMethodPlayerPrivate
			case "missing end":
				payload.EndedAt = nil
			case "newer positive":
				if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_reconciliation_heads
                    (video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at)
                    VALUES ($1,'UPCOMING',$2,$2)`, testVideoID, proof.ScheduledFor.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			case "newer pending":
				if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_pending_ends
                    (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
                    VALUES ($1,$2,'EXPLICIT_END',900001,$3,$3,$3,true,true)`, testVideoID, testChannelID, proof.ScheduledFor.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}

			publishLiveCheck(t.Context(), t, repo, lifecycleVideoEnvelope(t, &proof, payload))
			consumeLiveChecks(t.Context(), t, consumer)
			assertLifecycleOrigin(t, pool, "legacy_unknown")

			if status := liveSessionStatus(t, pool); status != "UPCOMING" {
				t.Fatalf("ambiguous check changed status: %s", status)
			}

			assertTableCount(t, pool, "youtube_notification_outbox", 0)
		})
	}
}

func TestVideoLifecycleUnknownReviewCASAndNewFactsInvalidateReceipt(t *testing.T) {
	pool, repo, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
	ctx := t.Context()
	publishLiveCheck(ctx, t, repo, lifecycleVideoEnvelope(t, &proof, unknownVideoCheck(contract.LiveCheckReasonIdentityMissing)))
	consumeLiveChecks(ctx, t, consumer)

	var expected string

	if err := pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM youtube_live_review_snapshot($1)`, testVideoID).Scan(&expected); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_sessions SET title='changed snapshot' WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	if err := recordLifecycleReview(t, pool, expected); err == nil {
		t.Fatal("stale snapshot must not create a review receipt")
	}

	if err := pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM youtube_live_review_snapshot($1)`, testVideoID).Scan(&expected); err != nil {
		t.Fatal(err)
	}

	if err := recordLifecycleReview(t, pool, expected); err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, pool, "youtube_live_review_receipts", 1)
	assertLifecycleOrigin(t, pool, "legacy_unknown")
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_review_receipts SET reason='rewritten' WHERE video_id=$1`, testVideoID); err == nil {
		t.Fatal("review receipt must reject updates even from its owner")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_review_receipts WHERE video_id=$1`, testVideoID); err == nil {
		t.Fatal("review receipt must reject deletion even from its owner")
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishLiveCheck(ctx, t, repo, lifecycleVideoEnvelope(t, &proof, endedVideoCheck(proof.ScheduledFor.Add(-time.Second))))
	consumeLiveChecks(ctx, t, consumer)

	var matches int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_live_review_receipts receipt
        WHERE video_id=$1 AND snapshot_sha256=(SELECT snapshot_sha256 FROM youtube_live_review_snapshot($1))`, testVideoID).Scan(&matches); err != nil {
		t.Fatal(err)
	}

	if matches != 0 {
		t.Fatal("new terminal fact was exempted by an old review")
	}

	assertLifecycleOrigin(t, pool, "observed")
	assertTableCount(t, pool, "youtube_live_review_receipts", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
}

func recordLifecycleReview(t *testing.T, pool *pgxpool.Pool, digest string) error {
	t.Helper()

	ctx := t.Context()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `SELECT record_youtube_live_review('10000000-0000-0000-0000-000000000001',$1,$2,'test-operator','검증된 UNKNOWN 검토 종료')`, testVideoID, digest)
	if err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}

		return err
	}

	return tx.Commit(ctx)
}
