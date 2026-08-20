package dbtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	reaperRecoveryAttempts         = 2
	containerStartRetryAttempts    = 5
	defaultContainerStartRetryWait = 500 * time.Millisecond
)

var containerStartRetryInterval = defaultContainerStartRetryWait

func provisionPostgresContainer(
	ctx context.Context,
	image string,
	start func(context.Context, string) (*postgres.PostgresContainer, error),
	holdReaper func(context.Context) error,
	verifyReaper func(context.Context) error,
) (*postgres.PostgresContainer, error) {
	var attemptErr error
	for range postgresProvisionAttempts() {
		container, err, retry := runPostgresProvisionAttempt(ctx, image, start, holdReaper, verifyReaper)
		if err == nil {
			return container, nil
		}
		attemptErr = errors.Join(attemptErr, err)
		if !retry {
			return nil, attemptErr
		}
	}
	return nil, attemptErr
}

func postgresProvisionAttempts() int {
	if reaperRecoveryAttempts > containerStartRetryAttempts {
		return reaperRecoveryAttempts
	}
	return containerStartRetryAttempts
}

func runPostgresProvisionAttempt(
	ctx context.Context,
	image string,
	start func(context.Context, string) (*postgres.PostgresContainer, error),
	holdReaper func(context.Context) error,
	verifyReaper func(context.Context) error,
) (*postgres.PostgresContainer, error, bool) {
	container, err, retry := tryStartPostgres(ctx, image, start, holdReaper)
	if err != nil {
		return nil, err, retry
	}
	return tryVerifyPostgres(ctx, container, verifyReaper)
}

func tryStartPostgres(
	ctx context.Context,
	image string,
	start func(context.Context, string) (*postgres.PostgresContainer, error),
	holdReaper func(context.Context) error,
) (*postgres.PostgresContainer, error, bool) {
	_ = holdReaper(ctx)
	container, err := start(ctx, image)
	if err == nil {
		return container, nil, false
	}
	if !isTransientContainerStartError(err) {
		return nil, err, false
	}
	waitErr := waitContainerStartRetry(ctx)
	if waitErr != nil {
		return nil, errors.Join(err, waitErr), false
	}
	return nil, err, true
}

func tryVerifyPostgres(
	ctx context.Context,
	container *postgres.PostgresContainer,
	verifyReaper func(context.Context) error,
) (*postgres.PostgresContainer, error, bool) {
	verifyErr := verifyReaper(ctx)
	if verifyErr == nil {
		return container, nil, false
	}
	wrapped := fmt.Errorf("verify reaper client registration: %w", verifyErr)
	retryErr := preparePostgresRetry(ctx, container, verifyErr)
	if retryErr != nil {
		return nil, errors.Join(wrapped, retryErr), false
	}
	return nil, wrapped, true
}

func isTransientContainerStartError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "marked for removal") {
		return true
	}
	return strings.Contains(msg, "removal") && strings.Contains(msg, "already in progress")
}

func waitContainerStartRetry(ctx context.Context) error {
	if containerStartRetryInterval <= 0 {
		return nil
	}
	timer := time.NewTimer(containerStartRetryInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func preparePostgresRetry(
	ctx context.Context,
	container *postgres.PostgresContainer,
	verifyErr error,
) error {
	if terminateErr := container.Terminate(ctx); terminateErr != nil {
		return fmt.Errorf("terminate unverified postgres container: %w", terminateErr)
	}
	if !isTransientReaperError(verifyErr) {
		return errors.New("reaper registration failure is not transient")
	}
	return nil
}

func startPostgresContainer(ctx context.Context, image string) (*postgres.PostgresContainer, error) {
	return postgres.Run(ctx, image,
		postgres.WithDatabase("dbtest"),
		postgres.WithUsername("dbtest"),
		postgres.WithPassword("dbtest"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
}
