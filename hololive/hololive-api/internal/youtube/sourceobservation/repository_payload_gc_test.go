package sourceobservation

import (
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestPayloadGCConcurrentPublishNeverLosesEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	first, err := publishkit.NewPublisher(repo.pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "same-post")))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM source_observations WHERE id = $1`, first.Results[0].ObservationID); err != nil {
		t.Fatal(err)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	input := publishInput(communityEnvelope(t, &proof, "same-post"))
	start := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		<-start

		_, publishErr := publishkit.NewPublisher(repo.pool).PublishBatch(ctx, input)
		result <- publishErr
	}()

	close(start)

	_, gcErr := repo.deleteUnreferencedPayloadBatch(ctx, RetentionConfig{
		BatchSize: 1, EvidenceAgeByKind: map[contract.ObservationKind]time.Duration{contract.KindCommunityPage: time.Hour},
	}, time.Now().Add(2*time.Hour))
	if gcErr != nil {
		t.Fatal(gcErr)
	}

	if err := <-result; err != nil {
		t.Fatalf("publish racing payload GC: %v", err)
	}

	var observations, payloads int

	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observations),
		       (SELECT count(*) FROM source_observation_payloads)
	`).Scan(&observations, &payloads); err != nil {
		t.Fatal(err)
	}

	if observations != 1 || payloads != 1 {
		t.Fatalf("publish/GC left observations=%d payloads=%d", observations, payloads)
	}
}
