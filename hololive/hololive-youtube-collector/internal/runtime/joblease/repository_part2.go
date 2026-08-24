package joblease

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func releaseLeaseTx(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	reason ReleaseReason,
	delay time.Duration,
) error {
	var (
		failureCode, failureClass, failureDetail *string
		failureAt                                *time.Time
	)

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_lease_failure_lock_0144_14.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
	).Scan(&failureCode, &failureClass, &failureDetail, &failureAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("release collection job lease: lock job row: %w", err)
	}

	var jobKey string

	err = tx.QueryRow(
		ctx,
		mustSQL("repository_lease_release_0144_10.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		delay.Milliseconds(), string(reason.ErrorCode()),
	).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("release collection job lease: %w", err)
	}

	// 177 trigger가 renew DEFERRED를 legacy_collector로 덮어쓰므로 잠근 사전 값을 되돌린다.
	err = tx.QueryRow(ctx, mustSQL("repository_lease_release_restore_0144_13.sql"),
		jobKey, failureCode, failureClass, failureDetail, failureAt).Scan(&jobKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("release collection job lease: restore failure history: %w", err)
	}

	return nil
}

func (l *JobLease) finish(ctx context.Context, code string, retryAt time.Time, class, detail, action string) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("%s collection job lease: %w", action, ErrFenceLost)
	}

	var (
		jobKey string
		err    error
	)

	if action == "complete" {
		err = l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_complete_0144_11.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
			l.proof.ProjectionGeneration, l.proof.ScheduledFor).Scan(&jobKey)
	} else {
		minDelay := l.repository.config.MinRetryDelay
		maxDelay := l.repository.config.MaxRetryDelay

		err = l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_defer_0144_12.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
			l.proof.ProjectionGeneration, l.proof.ScheduledFor, retryAt, code, class, detail,
			minDelay.Milliseconds(), maxDelay.Milliseconds()).Scan(&jobKey)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("%s collection job lease: %w", action, err)
	}

	return nil
}

func deterministicJitter(proof *contract.LeaseProof, minimum, maximum time.Duration) time.Duration {
	if maximum <= minimum {
		return minimum
	}

	hash := fnv.New64a()

	_, _ = hash.Write(fmt.Appendf(nil, "%s\x00%d", proof.JobKey, proof.FenceEpoch))

	span := big.NewInt((maximum - minimum).Nanoseconds() + 1)
	offset := new(big.Int).SetUint64(hash.Sum64())
	offset.Mod(offset, span)

	return minimum + time.Duration(offset.Int64())
}
