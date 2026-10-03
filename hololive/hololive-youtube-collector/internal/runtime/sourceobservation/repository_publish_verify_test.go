package sourceobservation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func breakPublishFence(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases SET fence_epoch = fence_epoch + 1 WHERE job_key = $1
	`, proof.JobKey); err != nil {
		t.Fatalf("advance fence epoch: %v", err)
	}
}

func stalePublishProjection(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_projection_generations
		SET valid_until = clock_timestamp() - INTERVAL '1 second'
		WHERE generation = $1
	`, proof.ProjectionGeneration); err != nil {
		t.Fatalf("stale projection: %v", err)
	}
}

func breakPublishMembership(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases SET subject_key = 'UC_OTHER' WHERE job_key = $1
	`, proof.JobKey); err != nil {
		t.Fatalf("move job subject: %v", err)
	}
}

func disablePublishTarget(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_targets SET enabled = FALSE WHERE projection_generation = $1
	`, proof.ProjectionGeneration); err != nil {
		t.Fatalf("disable target: %v", err)
	}
}

func stalePublishContract(ctx context.Context, t *testing.T, pool *pgxpool.Pool, _ *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_generation = 2
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'
	`); err != nil {
		t.Fatalf("stale contract: %v", err)
	}
}

// 검증 조회를 한 번에 보내도 판정 우선순위(fence → projection → job membership → target → contract)는
// 순차 실행과 같아야 한다. 여러 실패를 동시에 만들고 가장 앞선 실패만 보고되는지 확인한다. 자기 target 비활성은
// lease membership 범위의 행 수를 바꾸므로 target 조회보다 앞선 membership 판정에서 거절된다.
func TestPublishVerificationKeepsFailurePrecedence(t *testing.T) {
	type breaker func(context.Context, *testing.T, *pgxpool.Pool, *contract.LeaseProof)

	cases := []struct {
		name     string
		breakers []breaker
		want     error
		contains string
		excludes string
	}{
		{
			name:     "fence_before_projection_and_target",
			breakers: []breaker{breakPublishFence, stalePublishProjection, disablePublishTarget, stalePublishContract},
			want:     ErrCollectionFenceLost,
		},
		{
			name:     "projection_before_membership_and_target",
			breakers: []breaker{stalePublishProjection, breakPublishMembership, disablePublishTarget, stalePublishContract},
			want:     ErrProjectionStale,
		},
		{
			name:     "membership_before_target",
			breakers: []breaker{breakPublishMembership, disablePublishTarget, stalePublishContract},
			want:     ErrTargetDisabled,
			contains: "verify lease membership",
			excludes: "verify collection targets",
		},
		{
			name:     "own_target_disable_before_contract",
			breakers: []breaker{disablePublishTarget, stalePublishContract},
			want:     ErrTargetDisabled,
			contains: "verify lease membership",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
			input := publishInput(communityEnvelope(t, &proof, "post-1"))

			for _, apply := range tc.breakers {
				apply(ctx, t, pool, &input.Lease)
			}

			_, err := NewRepository(pool).PublishBatch(ctx, input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("publish error = %v, want %v", err, tc.want)
			}

			if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("publish error = %v, want %q", err, tc.contains)
			}

			if tc.excludes != "" && strings.Contains(err.Error(), tc.excludes) {
				t.Fatalf("publish error = %v, must not report %q first", err, tc.excludes)
			}

			assertPublishSideEffects(t, pool, 0, 0, 0)
		})
	}
}
