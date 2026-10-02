package sourceobservation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func TestPayloadNullOrMissingReferenceIsRejected(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	published, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "same-post")))
	if err != nil {
		t.Fatal(err)
	}

	id := published.Results[0].ObservationID
	if _, err := pool.Exec(ctx, `UPDATE source_observations SET payload_id = NULL WHERE id = $1`, id); err == nil {
		t.Fatal("null payload reference accepted")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM source_observation_payloads`); err == nil {
		t.Fatal("referenced payload deleted")
	}

	var payloadID int64

	if err := pool.QueryRow(ctx, `SELECT payload_id FROM source_observations WHERE id = $1`, id).Scan(&payloadID); err != nil || payloadID == 0 {
		t.Fatalf("source observation reference not preserved: id=%d err=%v", payloadID, err)
	}
}

func TestPayloadConcurrentPublishSeesCommittedDictionaryAfterWaiting(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	firstProof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindChannelPhoto, testChannelID, "youtubejs_channel_metadata")
	secondProof := observationtest.SeedAdditionalLease(t, pool, &firstProof, contract.ProviderHolodex, contract.KindChannelPhoto, testChannelID, "holodex_metadata")
	first := observationtest.ChannelPhotoEnvelopeFor(t, &firstProof, testChannelID)
	second := *first

	second.Provider, second.Lease = contract.ProviderHolodex, secondProof

	second, err := contract.PrepareEnvelope(second)
	require.NoError(t, err)
	require.Equal(t, first.PayloadSHA256, second.PayloadSHA256)

	firstRepo, secondRepo := NewRepository(pool), NewRepository(pool)
	written, release := pausePayloadCommit(firstRepo)

	defer release()

	firstDone, secondDone := make(chan error, 1), make(chan error, 1)

	go func() { _, err := firstRepo.PublishBatch(ctx, publishInput(first)); firstDone <- err }()

	select {
	case <-written:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	go func() { _, err := secondRepo.PublishBatch(ctx, publishInput(&second)); secondDone <- err }()

	var earlyError error

	finishedEarly := false

	require.Eventually(t, func() bool {
		select {
		case earlyError = <-secondDone:
			finishedEarly = true
			return true
		default:
		}

		var waiting bool

		err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted
			AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)

		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.False(t, finishedEarly, "second publisher returned before dictionary lock release: %v", earlyError)
	release()
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	var observations, payloads int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT payload_id) FROM source_observations`).Scan(&observations, &payloads))
	require.Equal(t, 2, observations)
	require.Equal(t, 1, payloads)
}

// pausePayloadCommit은 첫 publisher의 미commit 사전을 다른 publisher가 기다리게 합니다.
func pausePayloadCommit(repo *Repository) (<-chan struct{}, func()) {
	written, unblock := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })

	repo.publishFault = func(ctx context.Context, _ dbx.Tx, point publishFaultPoint) error {
		if point != faultAfterObservationSet {
			return nil
		}

		close(written)

		select {
		case <-unblock:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return written, release
}
