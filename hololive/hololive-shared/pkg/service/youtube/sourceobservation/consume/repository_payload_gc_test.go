package consume

import (
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func TestPayloadGCConcurrentPublishNeverLosesEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	first, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "same-post")))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM source_observations WHERE id = $1`, first.Results[0].ObservationID); err != nil {
		t.Fatal(err)
	}

	proof = observationtest.AdvanceLease(ctx, t, pool, &proof, time.Minute)

	input := publishInput(observationtest.CommunityEnvelope(t, &proof, "same-post"))
	start := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		<-start

		_, publishErr := repo.PublishBatch(ctx, input)
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
