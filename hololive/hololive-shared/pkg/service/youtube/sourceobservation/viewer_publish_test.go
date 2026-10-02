package sourceobservation

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

// 과거 생산자의 계약으로 보존 관측을 시드합니다. 현재 운영 publisher에는 적용하지 않습니다.
func historicalViewerJobContracts() StaticJobContracts {
	jobs := InitialJobContracts()
	youtubejs := mustJobID(contract.ProviderYouTubeJS, "youtubejs_viewer")

	jobs[youtubejs] = mustJobContract(youtubejs, JobClassSubject, JobMembershipExactSubject, "",
		[]contract.ObservationKind{contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindViewerSample}, nil)

	holodex := mustJobID(contract.ProviderHolodex, "holodex_live")

	jobs[holodex] = mustJobContract(holodex, JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_live",
		[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample},
		[]contract.ObservationKind{contract.KindLiveSnapshot})

	return jobs
}

func historicalViewerPublisher(pool *pgxpool.Pool) *Repository {
	return NewRepositoryWithContracts(pool, historicalViewerJobContracts(), nil)
}

func TestPublishRejectsRetiredViewerCollectionWithoutSideEffects(t *testing.T) {
	for _, tc := range []struct {
		provider contract.Provider
		job      string
		wantErr  error
	}{
		{contract.ProviderYouTubeJS, "youtubejs_viewer", ErrCollectionFenceLost},
		{contract.ProviderHolodex, "holodex_live", ErrTargetDisabled},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			proof := observationtest.SeedPublishLease(t.Context(), t, pool, tc.provider, contract.KindViewerSample, "video-1", tc.job)
			historical := observationtest.ViewerEnvelope(t, &proof, 1, 100)

			envelope, err := contract.PrepareEnvelope(contract.Envelope{
				Provider: tc.provider, ObservationKind: contract.KindViewerSample, SubjectKey: historical.SubjectKey,
				SchemaVersion: historical.SchemaVersion, ContractGeneration: historical.ContractGeneration,
				ScheduledFor: historical.ScheduledFor, ObservedAt: historical.ObservedAt,
				Completeness: historical.Completeness, Continuity: historical.Continuity,
				Payload: historical.Payload, CollectorInstance: proof.OwnerInstance, Lease: proof,
			})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := NewRepository(pool).PublishBatch(t.Context(), publishInput(&envelope)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("retired viewer publish error = %v, want %v", err, tc.wantErr)
			}

			assertPublishSideEffects(t, pool, 0, 0, 0)

			var state string

			if err := pool.QueryRow(t.Context(), `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
				t.Fatal(err)
			}

			if state != "ACTIVE" {
				t.Fatalf("rejected viewer publish changed lease to %s", state)
			}
		})
	}
}
