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
		container, retry, err := runPostgresProvisionAttempt(ctx, image, start, holdReaper, verifyReaper)
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
) (*postgres.PostgresContainer, bool, error) {
	container, retry, err := tryStartPostgres(ctx, image, start, holdReaper)
	if err != nil {
		return nil, retry, fmt.Errorf("try start postgres: %w", err)
	}

	verified, verifyRetry, verifyErr := tryVerifyPostgres(ctx, container, verifyReaper)
	if verifyErr != nil {
		return nil, verifyRetry, fmt.Errorf("try verify postgres: %w", verifyErr)
	}

	return verified, verifyRetry, nil
}

func tryStartPostgres(
	ctx context.Context,
	image string,
	start func(context.Context, string) (*postgres.PostgresContainer, error),
	holdReaper func(context.Context) error,
) (*postgres.PostgresContainer, bool, error) {
	if holdErr := holdReaper(ctx); shouldFailClosedOnHold(holdErr) {
		return nil, false, fmt.Errorf("hold session reaper: %w", holdErr)
	}

	container, err := start(ctx, image)
	if err == nil {
		if container == nil {
			return nil, false, errors.New("postgres container start returned nil")
		}

		return container, false, nil
	}

	if !isTransientContainerStartError(err) {
		return nil, false, fmt.Errorf("start postgres container: %w", err)
	}

	waitErr := waitContainerStartRetry(ctx)
	if waitErr != nil {
		return nil, false, errors.Join(err, waitErr)
	}

	return nil, true, fmt.Errorf("start postgres container: %w", err)
}

func tryVerifyPostgres(
	ctx context.Context,
	container *postgres.PostgresContainer,
	verifyReaper func(context.Context) error,
) (*postgres.PostgresContainer, bool, error) {
	verifyErr := verifyReaper(ctx)
	if verifyErr == nil {
		return container, false, nil
	}

	wrapped := fmt.Errorf("verify reaper client registration: %w", verifyErr)

	retryErr := preparePostgresRetry(ctx, container, verifyErr)
	if retryErr != nil {
		return nil, false, errors.Join(wrapped, retryErr)
	}

	return nil, true, wrapped
}

func shouldFailClosedOnHold(err error) bool {
	if err == nil {
		return false
	}

	if isTransientReaperError(err) {
		return false
	}

	return !errors.Is(err, context.DeadlineExceeded)
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
		if err := context.Cause(ctx); err != nil {
			return fmt.Errorf("cause: %w", err)
		}

		return nil
	case <-timer.C:
		return nil
	}
}

func preparePostgresRetry(
	ctx context.Context,
	container *postgres.PostgresContainer,
	verifyErr error,
) error {
	if container == nil {
		return errors.New("unverified postgres container is missing")
	}

	if terminateErr := container.Terminate(ctx); terminateErr != nil {
		return fmt.Errorf("terminate unverified postgres container: %w", terminateErr)
	}

	if !isTransientReaperError(verifyErr) {
		return errors.New("reaper registration failure is not transient")
	}

	return nil
}

func startPostgresContainer(ctx context.Context, image string) (*postgres.PostgresContainer, error) {
	out, err := postgres.Run(ctx, image,
		postgres.WithDatabase("dbtest"),
		postgres.WithUsername("dbtest"),
		postgres.WithPassword("dbtest"),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_INITDB_ARGS": "--locale-provider=builtin --builtin-locale=C.UTF-8 --encoding=UTF8 --data-checksums",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}

	return out, nil
}
