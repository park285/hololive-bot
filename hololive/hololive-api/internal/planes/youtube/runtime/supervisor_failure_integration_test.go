package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestFailureRecordingTimeoutRecoversExpiredLease(t *testing.T) {
	for _, operation := range []string{"retry", "dead_letter"} {
		t.Run(operation, func(t *testing.T) {
			testFailureRecordingTimeoutRecovery(t, operation)
		})
	}
}

// testFailureRecordingTimeoutRecovery는 기록 실패 뒤 다음 claim이 처리되고, 실패한 행은 PROCESSING으로 남았다가
// lease 만료 뒤 기존 claim 경로로 회수되는지 확인한다.
func testFailureRecordingTimeoutRecovery(t *testing.T, operation string) {
	t.Helper()

	pool := dbtest.NewPool(t)
	seedFailureRecoveryQueue(t, pool)

	repo := sourceobservation.NewRepository(pool)

	var (
		failedID int64
		attempts []int64
	)

	cause := context.DeadlineExceeded

	if operation == "dead_letter" {
		cause = errors.New("invalid observation")
	}

	r := newTestRuntime(failedRecordingRepository{Repository: repo}, fakeConsumer{
		consume: func(ctx context.Context, claim sourceobservation.Claim) error {
			attempts = append(attempts, claim.ObservationID)
			if len(attempts) == 1 {
				failedID = claim.ObservationID
				return cause
			}

			if err := sourceobservation.NewConsumer(repo).ConsumeClaim(ctx, claim); err != nil {
				return fmt.Errorf("consume recovered observation: %w", err)
			}

			return nil
		},
	})
	batch, err := r.claimObservationBatch(t.Context())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 2)

	for _, work := range batch.Claims {
		r.workCh <- work
	}

	close(r.workCh)

	errCh := make(chan error, 1)
	r.runWorker(t.Context(), errCh)
	require.Empty(t, errCh)
	require.Len(t, attempts, 2)

	var token string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT lease_token FROM source_observation_queue WHERE observation_id=$1`, failedID).Scan(&token))
	require.Equal(t, batch.Claims[0].LeaseToken, token)
	requireQueueStatus(t, pool, failedID, contract.StatusProcessing)
	requireQueueStatus(t, pool, attempts[1], contract.StatusProcessed)
	requireExpiredLeaseRecovered(t, r, pool, failedID, token)
}

func requireExpiredLeaseRecovered(t *testing.T, r *Runtime, pool *pgxpool.Pool, failedID int64, failedToken string) {
	t.Helper()

	beforeExpiry, err := r.claimObservationBatch(t.Context())
	require.NoError(t, err)
	require.Empty(t, beforeExpiry.Claims)

	// 긴 실제 lease를 기다리지 않고 격리 DB에서 만료 시각만 앞당깁니다.
	_, err = pool.Exec(t.Context(), `UPDATE source_observation_queue SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE observation_id=$1`, failedID)
	require.NoError(t, err)

	recovered, err := r.claimObservationBatch(t.Context())
	require.NoError(t, err)
	require.Len(t, recovered.Claims, 1)

	work := recovered.Claims[0]
	require.Equal(t, failedID, work.ObservationID)
	require.Equal(t, 2, work.AttemptCount)
	require.NotEqual(t, failedToken, work.LeaseToken)
	require.NoError(t, r.processClaim(t.Context(), work))
	requireQueueStatus(t, pool, failedID, contract.StatusProcessed)
}

func requireQueueStatus(t *testing.T, pool *pgxpool.Pool, observationID int64, want contract.Status) {
	t.Helper()

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status FROM source_observation_queue WHERE observation_id=$1`, observationID).Scan(&status))
	require.Equal(t, string(want), status)
}

type failedRecordingRepository struct {
	*sourceobservation.Repository
}

func (failedRecordingRepository) Retry(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
	return "", context.DeadlineExceeded
}

func (failedRecordingRepository) DeadLetter(context.Context, sourceobservation.DeadLetterInput) error {
	return context.DeadlineExceeded
}

func seedFailureRecoveryQueue(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: "UC_RECOVERY",
		Coverage:  contract.CommunityPageCoverageV1{ChannelID: "UC_RECOVERY", MaxResults: 10, PageCount: 1, Exhausted: true},
	})
	require.NoError(t, err)

	for i := range 2 {
		scheduled := time.Now().UTC().Truncate(time.Second).Add(time.Duration(i-2) * time.Minute)
		envelope, err := contract.PrepareEnvelope(contract.Envelope{
			Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindCommunityPage,
			SubjectKey: "UC_RECOVERY", SchemaVersion: 1, ContractGeneration: 1,
			ScheduledFor: scheduled, ObservedAt: scheduled,
			Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
			Payload: payload, CollectorInstance: "dbtest",
			Lease: contract.LeaseProof{
				JobKey: "recovery-job", CollectionJobKind: "community_collect",
				OwnerInstance: "dbtest", FenceEpoch: 1, ProjectionGeneration: 1, ScheduledFor: scheduled,
			},
		})
		require.NoError(t, err)

		_, err = pool.Exec(t.Context(), `
			WITH payload AS (
			 INSERT INTO source_observation_payloads
			 (observation_kind, schema_version, canonical_profile, payload_sha256, payload)
			 VALUES ('community_page', 1, 'source-observation-canonical-json-v1', decode($1,'hex'), $2)
			 ON CONFLICT (observation_kind,schema_version,canonical_profile,payload_sha256)
			 DO UPDATE SET payload=EXCLUDED.payload
			 RETURNING id
			), observation AS (
			 INSERT INTO source_observations
			 (provider, observation_kind, subject_key, observation_key, schema_version,
			  contract_generation, scheduled_for, observed_at, scope_sha256, completeness,
			  continuity, payload_id, evidence_sha256, collector_instance, job_key,
			  collection_job_kind, fence_epoch, projection_generation)
			 SELECT 'youtubejs', 'community_page', 'UC_RECOVERY', $3, 1,
			  1, $4, $4, $5, 'COMPLETE', 'CONTIGUOUS', payload.id,
			  $6, 'dbtest', 'recovery-job', 'community_collect', 1, 1 FROM payload
			 RETURNING id
			)
			INSERT INTO source_observation_queue(observation_id) SELECT id FROM observation`,
			envelope.PayloadSHA256, []byte(envelope.Payload), envelope.ObservationKey, scheduled,
			envelope.ScopeSHA256, envelope.EvidenceSHA256)
		require.NoError(t, err)
	}
}
