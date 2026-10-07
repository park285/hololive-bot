package sourceobservation

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func startUnobservedVideoLifecycle(t *testing.T, origin string) (*pgxpool.Pool, *Consumer, contract.LeaseProof) {
	t.Helper()

	pool, _, consumer, proof := startLivePersist(t)

	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_sessions
        (video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
        VALUES ($1,$2,'UPCOMING',NULL,now()-interval '1 year',$3)`, testVideoID, testChannelID, origin)
	if err != nil {
		t.Fatal(err)
	}

	proof = seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

	return pool, consumer, proof
}

func TestVideoLifecycleSettlesUnobservedEndWithoutStartOrNotification(t *testing.T) {
	for _, origin := range []string{"metadata_only", "legacy_unknown"} {
		t.Run(origin, func(t *testing.T) {
			pool, consumer, proof := startUnobservedVideoLifecycle(t, origin)
			ended := proof.ScheduledFor.Add(-time.Minute)
			id := publishLiveCheck(t.Context(), t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &proof, endedVideoCheck(ended)))
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
			pool, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
			payload := endedVideoCheck(proof.ScheduledFor)

			payload.EndedAt = nil
			payload.IsUpcoming = new(true)

			if confirmed {
				payload.WaitingStateConfirmed = new(true)
				payload.ScheduledAt = new(proof.ScheduledFor.Add(time.Hour))
			}

			publishLiveCheck(t.Context(), t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &proof, payload))
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
	for _, scenario := range []string{"channel mismatch", "private only", "missing end", "newer positive", "positive after end", "newer pending"} {
		t.Run(scenario, func(t *testing.T) {
			pool, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
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
			case "positive after end":
				// 확인 관측보다는 이르지만 ended_at보다 늦은 positive다. live clock이 없는 시작 미관측 경로는
				// grace 없이 바로 끝내므로 consumer가 ended_at과 직접 비교해 거부해야 한다.
				if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_reconciliation_heads
                    (video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at)
                    VALUES ($1,'UPCOMING',$2,$2)`, testVideoID, proof.ScheduledFor.Add(-30*time.Second)); err != nil {
					t.Fatal(err)
				}
			case "newer pending":
				if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_pending_ends
                    (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
                    VALUES ($1,$2,'EXPLICIT_END',900001,$3,$3,$3,true,true)`, testVideoID, testChannelID, proof.ScheduledFor.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}

			id := publishLiveCheck(t.Context(), t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &proof, payload))
			consumeLiveChecks(t.Context(), t, consumer)
			assertLifecycleOrigin(t, pool, "legacy_unknown")

			if status := liveSessionStatus(t, pool); status != "UPCOMING" {
				t.Fatalf("ambiguous check changed status: %s", status)
			}

			if scenario == "positive after end" {
				assertApplicationDecision(t, pool, id, liveSessionEntityKind, videoLifecycleInvalidEnd)
			}

			assertTableCount(t, pool, "youtube_notification_outbox", 0)
		})
	}
}

// startLiveWithLaggedPositive는 시작을 관측한 LIVE를 만들고, 그 마지막 positive보다 30초 이른 검증된
// ended_at과 positive 다음 슬롯의 영상 확인 lease를 돌려준다. Holodex live_snapshot의 EffectiveAt은 수집 예정
// 시각이라 positive 시각이 YouTube의 실제 종료보다 늦어지는 운영 순서를 재현한다.
func startLiveWithLaggedPositive(t *testing.T, grace time.Duration) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof, time.Time) {
	t.Helper()

	pool, repo, consumer, proof := startLivePersistGrace(t, grace)
	positiveAt := proof.ScheduledFor
	positive := liveSession(testVideoID, testStatusLive)

	positive.StartedAt = new(positiveAt.Add(-2 * time.Hour))
	proof = publishConsumeLive(t.Context(), t, pool, publishkit.NewPublisher(pool), consumer, &proof, positive)

	endedAt := positiveAt.Add(-30 * time.Second)
	if got := loadVideoLifecycle(t, pool); got.status != testStatusLive || !got.livePositiveAt.After(endedAt) {
		t.Fatalf("fixture = %+v, want LIVE positive after ended_at %s", got, endedAt)
	}

	videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

	return pool, repo, consumer, videoProof, endedAt
}

func TestVideoLifecycleLiveEndAcceptsPositiveAfterEndedAt(t *testing.T) {
	for _, laggedUpcoming := range []bool{false, true} {
		t.Run(map[bool]string{false: "lagged live positive", true: "lagged upcoming positive"}[laggedUpcoming], func(t *testing.T) {
			pool, _, consumer, videoProof, endedAt := startLiveWithLaggedPositive(t, 0)
			ctx := t.Context()

			if laggedUpcoming {
				// 시작을 관측한 LIVE의 일반 명시적 종료는 UPCOMING positive를 종료 차단 근거로 보지 않는다.
				if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
                    SET last_upcoming_positive_at=$2,last_upcoming_positive_seen_at=$2 WHERE video_id=$1`,
					testVideoID, videoProof.ScheduledFor.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
			}

			id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
			consumeLiveChecks(ctx, t, consumer)

			got := loadVideoLifecycle(t, pool)
			if got.status != testStatusEnded || got.headStatus != testStatusEnded || !got.endedAt.Equal(endedAt) ||
				got.endReason != string(live.EndReasonExplicitEnd) {
				t.Fatalf("lifecycle = %+v, want ENDED at verified %s", got, endedAt)
			}

			assertApplicationDecision(t, pool, id, liveSessionEntityKind, "ENDED")
			assertTableCount(t, pool, "youtube_live_pending_ends", 0)
			assertTableCount(t, pool, "youtube_notification_outbox", 0)
		})
	}
}

// grace 안에 받은 positive는 즉시 종료를 막고, 같은 pending을 기존 finalizer가 검증된 ended_at으로 정산한다.
func TestVideoLifecycleLiveEndWaitsForGraceAfterLaggedPositive(t *testing.T) {
	const grace = time.Hour

	pool, repo, consumer, videoProof, endedAt := startLiveWithLaggedPositive(t, grace)
	ctx := t.Context()

	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
	consumeLiveChecks(ctx, t, consumer)

	var seenAt, nextCheck *time.Time

	if err := pool.QueryRow(ctx, `SELECT last_live_positive_seen_at,next_end_check_at
        FROM youtube_live_reconciliation_heads WHERE video_id=$1`, testVideoID).Scan(&seenAt, &nextCheck); err != nil {
		t.Fatal(err)
	}

	got := loadVideoLifecycle(t, pool)
	if got.status != testStatusLive || got.candidate != string(live.EndEvidenceExplicitEnd) ||
		seenAt == nil || !timeValue(nextCheck).Equal(seenAt.Add(grace)) {
		t.Fatalf("lifecycle inside grace = %+v next_end_check_at=%v seen=%v", got, nextCheck, seenAt)
	}

	if pending := loadPendingEnd(t, pool); pending.observationID != id || !pending.effectiveAt.Equal(videoProof.ScheduledFor) || !pending.endedAt.Equal(endedAt) {
		t.Fatalf("pending end = %+v, want check %d at %s ended %s", pending, id, videoProof.ScheduledFor, endedAt)
	}

	assertApplicationDecision(t, pool, id, liveSessionEntityKind, "END_CANDIDATE")

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
        SET last_live_positive_seen_at=NOW()-INTERVAL '2 hours',next_end_check_at=NOW() WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	if processed, err := repo.FinalizeNextDueLiveEnd(ctx, grace); err != nil || !processed {
		t.Fatalf("finalize due: processed=%t err=%v", processed, err)
	}

	if got := loadVideoLifecycle(t, pool); got.status != testStatusEnded || got.headStatus != testStatusEnded || !got.endedAt.Equal(endedAt) {
		t.Fatalf("finalizer lifecycle = %+v, want ENDED at verified %s", got, endedAt)
	}

	assertTableCount(t, pool, "youtube_live_pending_ends", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
}

// 확인 관측 시각과 같거나 늦은 positive는 확인 뒤에도 방송이 이어졌다는 근거이므로 종료 사실을 버린다.
func TestVideoLifecycleLiveEndRetainsPositiveAtCheckTime(t *testing.T) {
	pool, _, consumer, videoProof, endedAt := startLiveWithLaggedPositive(t, 0)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=$2 WHERE video_id=$1`,
		testVideoID, videoProof.ScheduledFor); err != nil {
		t.Fatal(err)
	}

	before := loadVideoLifecycle(t, pool)
	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
	consumeLiveChecks(ctx, t, consumer)

	if after := loadVideoLifecycle(t, pool); after != before {
		t.Fatalf("check at the positive slot changed lifecycle: %+v -> %+v", before, after)
	}

	assertApplicationDecision(t, pool, id, liveSessionEntityKind, videoLifecycleNewerEndRetained)
	assertTableCount(t, pool, "youtube_live_pending_ends", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
}

func TestVideoLifecycleUnknownReviewCASAndNewFactsInvalidateReceipt(t *testing.T) {
	pool, consumer, proof := startUnobservedVideoLifecycle(t, "legacy_unknown")
	ctx := t.Context()
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &proof, unknownVideoCheck(contract.LiveCheckReasonIdentityMissing)))
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
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &proof, endedVideoCheck(proof.ScheduledFor.Add(-time.Second))))
	consumeLiveChecks(ctx, t, consumer)

	var exempted bool

	if err := pool.QueryRow(ctx, `SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt($1)`, testVideoID).Scan(&exempted); err != nil {
		t.Fatal(err)
	}

	if exempted {
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
