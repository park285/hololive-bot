package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

const testCooldownDeferredState = "DEFERRED"

func TestPartialCooldownPreservesNotBefore(t *testing.T) {
	for _, past := range []bool{false, true} {
		name := "future beyond retry maximum"

		if past {
			name = "past uses database minimum"
		}

		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

			var before, after time.Time

			if err := pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&before); err != nil {
				t.Fatal(err)
			}

			at := before.Add(10 * time.Minute)

			if past {
				at = before.Add(-time.Hour)
			}

			input := partialCooldownInput(t, at)
			if _, err := NewRepository(pool).PublishBatchAndDefer(t.Context(), publishInput(communityEnvelope(t, &proof, "post-1")), input); err != nil {
				t.Fatal(err)
			}

			if err := pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&after); err != nil {
				t.Fatal(err)
			}

			got := readLeaseTerminal(t.Context(), t, pool, proof.JobKey)
			if got.state != testCooldownDeferredState || got.retryAt == nil || !got.scheduledFor.Equal(proof.ScheduledFor) ||
				got.failureCode != string(contract.ErrorCooldown) || got.failureClass != string(contract.ClassCooldown) {
				t.Fatalf("partial cooldown lost terminal contract: %+v", got)
			}

			if past {
				if got.retryAt.Before(before.Add(input.Bounds().Minimum)) || got.retryAt.After(after.Add(input.Bounds().Minimum)) {
					t.Fatalf("partial cooldown = %s, want database now + minimum", got.retryAt)
				}
			} else if !got.retryAt.Equal(input.Schedule().At()) {
				t.Fatalf("partial cooldown = %s, want %s", got.retryAt, input.Schedule().At())
			}

			assertPublishSideEffects(t, pool, 1, 1, 1)
		})
	}
}

func partialCooldownInput(t *testing.T, at time.Time) collection.DeferCollectionInput {
	t.Helper()

	diagnostic, err := contract.NewFailureDiagnostic(contract.ErrorCooldown, contract.ClassCooldown, "provider cooldown")
	if err != nil {
		t.Fatal(err)
	}

	schedule, err := collection.NewRetryNotBeforeSchedule(at)
	if err != nil {
		t.Fatal(err)
	}

	input, err := collection.NewDeferCollectionInput(diagnostic, collection.RetryBounds{Minimum: 200 * time.Millisecond, Maximum: time.Second}, schedule)
	if err != nil {
		t.Fatal(err)
	}

	return input
}

func TestPartialCooldownCancellationRollsBackTerminalAndObservations(t *testing.T) {
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := NewRepository(pool)
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	terminalReached := false

	repo.publishFault = func(ctx context.Context, tx dbx.Tx, point publishFaultPoint) error {
		if point != faultBeforeCommit {
			return nil
		}

		var state string

		if err := tx.QueryRow(ctx, `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
			return fmt.Errorf("read uncommitted defer: %w", err)
		}

		terminalReached = state == testCooldownDeferredState

		cancel()

		return ctx.Err()
	}

	_, err := repo.PublishBatchAndDefer(ctx, publishInput(communityEnvelope(t, &proof, "post-1")), partialCooldownInput(t, time.Now().Add(10*time.Minute)))
	if !errors.Is(err, context.Canceled) || !terminalReached {
		t.Fatalf("terminal cancellation = %v, reached defer=%t", err, terminalReached)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)

	got := readLeaseTerminal(t.Context(), t, pool, proof.JobKey)
	if got.state != testSlotStateActive || got.retryAt != nil || got.failureCode != "" || !got.scheduledFor.Equal(proof.ScheduledFor) {
		t.Fatalf("canceled partial cooldown committed: %+v", got)
	}
}

func TestPartialDeferSQLRejectsNonCooldownNotBefore(t *testing.T) {
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	var jobKey string

	err := pool.QueryRow(t.Context(), sqlJobDefer,
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "failure", time.Now().Add(10*time.Minute), int64(100), int64(1000), true,
	).Scan(&jobKey)

	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("non-cooldown partial not-before SQL = %v, want no rows", err)
	}

	if got := readLeaseTerminal(t.Context(), t, pool, proof.JobKey); got.state != testSlotStateActive || got.failureCode != "" {
		t.Fatalf("rejected partial SQL mutated lease: %+v", got)
	}
}
