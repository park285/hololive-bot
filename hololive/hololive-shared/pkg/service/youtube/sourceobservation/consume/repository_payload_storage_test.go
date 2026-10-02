package consume

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func TestPayloadDictionaryKeepsIndependentObservationSlotsAndReplay(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	first := observationtest.CommunityEnvelope(t, &proof, "same-post")

	published, err := repo.PublishBatch(ctx, publishInput(first))
	require.NoError(t, err)

	secondProof := observationtest.AdvanceLease(ctx, t, pool, &proof, time.Minute)
	second := observationtest.CommunityEnvelope(t, &secondProof, "same-post")

	require.Equal(t, first.PayloadSHA256, second.PayloadSHA256)
	require.NotEqual(t, first.ObservationKey, second.ObservationKey)

	publishedAgain, err := repo.PublishBatch(ctx, publishInput(second))
	require.NoError(t, err)

	require.Equal(t, sourceobservation.PublishInserted, publishedAgain.Results[0].Outcome)
	require.NotEqual(t, published.Results[0].ObservationID, publishedAgain.Results[0].ObservationID)

	var observations, payloads, queue int

	err = pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observations),
		       (SELECT count(*) FROM source_observation_payloads),
		       (SELECT count(*) FROM source_observation_queue)
	`).Scan(&observations, &payloads, &queue)
	require.NoError(t, err)

	require.Equal(t, 2, observations)
	require.Equal(t, 1, payloads)
	require.Equal(t, 2, queue)

	claims, err := repo.ClaimBatch(ctx, claimOptions())
	require.NoError(t, err)
	require.Len(t, claims.Claims, 2)

	for _, work := range claims.Claims {
		claim := Claim{ConsumerName: claims.ConsumerName, ObservationID: work.ObservationID, LeaseToken: work.LeaseToken}

		_, err = repo.Finalize(ctx, claim, func(_ context.Context, _ dbx.Tx, observation *Observation) (ReconcileResult, error) {
			require.Equal(t, first.PayloadSHA256, observation.PayloadSHA256)
			require.JSONEq(t, string(first.Payload), string(observation.Payload))

			return ReconcileResult{}, nil
		})
		require.NoError(t, err)
	}

	result, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID, RequestedBy: testReplayOperator,
		Reason: "verify shared payload replay",
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
}

func TestPayloadDigestCollisionAndCorruptionFailClosed(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	first := observationtest.CommunityEnvelope(t, &proof, "same-post")

	published, err := repo.PublishBatch(ctx, publishInput(first))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `UPDATE source_observation_payloads SET payload = '{}'::jsonb`)
	require.NoError(t, err)

	proof = observationtest.AdvanceLease(ctx, t, pool, &proof, time.Minute)
	_, err = repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "same-post")))
	require.Error(t, err, "dictionary digest collision/content mismatch accepted")

	claims, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(claims.Claims) != 1 {
		t.Fatalf("claim surviving observation: %#v %v", claims, err)
	}

	work := claims.Claims[0]

	_, err = repo.Finalize(ctx, Claim{ConsumerName: claims.ConsumerName, ObservationID: work.ObservationID, LeaseToken: work.LeaseToken},
		func(_ context.Context, _ dbx.Tx, _ *Observation) (ReconcileResult, error) {
			t.Fatal("corrupt payload reached reconciliation")

			return ReconcileResult{}, nil
		})

	if !errors.Is(err, errStoredPayloadCorrupt) {
		t.Fatalf("claim corrupt payload error = %v", err)
	}

	if _, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID, RequestedBy: testReplayOperator, Reason: "corrupt payload",
	}); !errors.Is(err, errStoredPayloadCorrupt) {
		t.Fatalf("replay corrupt payload error = %v", err)
	}
}
