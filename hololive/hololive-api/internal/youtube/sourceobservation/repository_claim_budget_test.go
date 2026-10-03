package sourceobservation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const claimBudgetTransactionTimeout = 10 * time.Second

// claimBudgetQueueRow는 queue 행의 물리 버전(ctid)과 claim 필드를 문자열로 담아 "쓰기 없음"을 값 비교로 확인한다.
type claimBudgetQueueRow struct {
	ctid           string
	status         string
	leaseToken     string
	leaseExpiresAt string
	updatedAt      string
}

func readClaimBudgetQueueRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) claimBudgetQueueRow {
	t.Helper()

	var row claimBudgetQueueRow

	if err := pool.QueryRow(ctx, `
		SELECT ctid::text, status, COALESCE(lease_token, ''), COALESCE(lease_expires_at::text, ''), updated_at::text
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observationID).Scan(&row.ctid, &row.status, &row.leaseToken, &row.leaseExpiresAt, &row.updatedAt); err != nil {
		t.Fatalf("load claim budget queue row: %v", err)
	}

	return row
}

// setClaimLeaseRemaining은 DB 시계 기준으로 남은 lease를 맞춘다. 예산 경계 판정이 DB NOW()를 쓰기 때문이다.
func setClaimLeaseRemaining(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64, remaining time.Duration) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE source_observation_queue
		SET lease_expires_at = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
		WHERE observation_id = $1
	`, observationID, remaining.Milliseconds()); err != nil {
		t.Fatalf("set claim lease remaining: %v", err)
	}
}

func claimLeaseRemaining(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) time.Duration {
	t.Helper()

	var remainingMillis int64

	if err := pool.QueryRow(ctx, `
		SELECT (EXTRACT(EPOCH FROM lease_expires_at - clock_timestamp()) * 1000)::bigint
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observationID).Scan(&remainingMillis); err != nil {
		t.Fatalf("load claim lease remaining: %v", err)
	}

	return time.Duration(remainingMillis) * time.Millisecond
}

func claimBudgetFixture(t *testing.T) (*pgxpool.Pool, *Repository, Claim) {
	t.Helper()

	pool := dbtest.NewPool(t)
	batch := publishAndClaimCommunityPost(t.Context(), t, pool, claimOptions())

	return pool, NewRepository(pool), batch.Claims[0].Claim(batch.ConsumerName)
}

func TestEnsureClaimBudgetSkipsWriteWhileLeaseCoversBudget(t *testing.T) {
	ctx := t.Context()
	pool, repo, claim := claimBudgetFixture(t)
	budget := 2 * claimBudgetTransactionTimeout

	for _, remaining := range []time.Duration{budget + 2*time.Second, 3 * budget} {
		setClaimLeaseRemaining(ctx, t, pool, claim.ObservationID, remaining)

		before := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID)

		if err := repo.EnsureClaimBudget(ctx, claim, claimBudgetTransactionTimeout); err != nil {
			t.Fatalf("ensure budget with %s remaining: %v", remaining, err)
		}

		// 연장이 필요 없으면 새 튜플 버전(ctid 변경)도 lease·updated_at 변경도 없어야 한다.
		if after := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID); after != before {
			t.Fatalf("lease with %s remaining was rewritten: before=%+v after=%+v", remaining, before, after)
		}
	}
}

func TestEnsureClaimBudgetExtendsLeaseJustBelowBudget(t *testing.T) {
	ctx := t.Context()
	pool, repo, claim := claimBudgetFixture(t)
	budget := 2 * claimBudgetTransactionTimeout

	setClaimLeaseRemaining(ctx, t, pool, claim.ObservationID, budget-time.Second)

	before := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID)

	if err := repo.EnsureClaimBudget(ctx, claim, claimBudgetTransactionTimeout); err != nil {
		t.Fatalf("ensure budget just below threshold: %v", err)
	}

	after := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID)
	if after.leaseExpiresAt == before.leaseExpiresAt || after.updatedAt == before.updatedAt {
		t.Fatalf("short lease was not extended: before=%+v after=%+v", before, after)
	}

	if remaining := claimLeaseRemaining(ctx, t, pool, claim.ObservationID); remaining <= budget-time.Second || remaining > budget {
		t.Fatalf("extended lease remaining = %s, want within (%s, %s]", remaining, budget-time.Second, budget)
	}

	if after.status != string(contract.StatusProcessing) || after.leaseToken != claim.LeaseToken {
		t.Fatalf("extension changed claim ownership: %+v", after)
	}
}

func TestEnsureClaimBudgetReportsLostClaimWithoutWriting(t *testing.T) {
	cases := []struct {
		name  string
		setup func(context.Context, *testing.T, *pgxpool.Pool, *Claim)
	}{
		{
			name: "expired_lease",
			setup: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, claim *Claim) {
				t.Helper()
				setClaimLeaseRemaining(ctx, t, pool, claim.ObservationID, -time.Second)
			},
		},
		{
			name: "lease_token_mismatch",
			setup: func(_ context.Context, _ *testing.T, _ *pgxpool.Pool, claim *Claim) {
				claim.LeaseToken = strings.Repeat("f", 64)
			},
		},
		{
			name: "claim_released",
			setup: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, claim *Claim) {
				t.Helper()

				if _, err := pool.Exec(ctx, `
					UPDATE source_observation_queue
					SET status = 'PENDING', lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL
					WHERE observation_id = $1
				`, claim.ObservationID); err != nil {
					t.Fatalf("release claim: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			pool, repo, claim := claimBudgetFixture(t)

			tc.setup(ctx, t, pool, &claim)

			before := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID)

			err := repo.EnsureClaimBudget(ctx, claim, claimBudgetTransactionTimeout)
			if !errors.Is(err, ErrClaimLost) {
				t.Fatalf("ensure budget error = %v, want ErrClaimLost", err)
			}

			if after := readClaimBudgetQueueRow(ctx, t, pool, claim.ObservationID); after != before {
				t.Fatalf("lost claim was rewritten: before=%+v after=%+v", before, after)
			}
		})
	}
}
