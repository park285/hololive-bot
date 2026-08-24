package dbtest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIsTransientContainerStartError(t *testing.T) {
	t.Parallel()

	require.False(t, isTransientContainerStartError(nil))
	require.False(t, isTransientContainerStartError(errors.New("pull access denied")))
	require.True(t, isTransientContainerStartError(errors.New(
		"run postgres: generic container: start container: container start: Error response from daemon: container is marked for removal and cannot be started",
	)))
	require.True(t, isTransientContainerStartError(errors.New(
		"Error response from daemon: removal of container abc123 is already in progress",
	)))
}

func TestProvisionPostgresContainerRetriesMarkedForRemoval(t *testing.T) {
	prevInterval := containerStartRetryInterval

	containerStartRetryInterval = 0

	t.Cleanup(func() { containerStartRetryInterval = prevInterval })

	starts := 0
	holds := 0
	container, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			starts++
			if starts < 3 {
				return nil, errors.New("container start: Error response from daemon: container is marked for removal and cannot be started")
			}

			return &postgres.PostgresContainer{}, nil
		},
		func(context.Context) error {
			holds++
			return nil
		},
		func(context.Context) error { return nil },
	)

	require.NoError(t, err)
	require.NotNil(t, container)
	require.Equal(t, 3, starts)
	require.Equal(t, 3, holds)
}

func TestProvisionPostgresContainerDoesNotRetryPermanentStartError(t *testing.T) {
	wantErr := errors.New("pull access denied for postgres:test")
	starts := 0
	_, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			starts++
			return nil, wantErr
		},
		func(context.Context) error { return nil },
		func(context.Context) error {
			t.Fatal("verify must not run after a permanent start failure")

			return nil
		},
	)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, 1, starts)
}

func TestProvisionPostgresContainerExhaustsTransientStartRetries(t *testing.T) {
	prevInterval := containerStartRetryInterval

	containerStartRetryInterval = 0

	t.Cleanup(func() { containerStartRetryInterval = prevInterval })

	starts := 0
	_, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			starts++
			return nil, errors.New("container is marked for removal and cannot be started")
		},
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	require.Error(t, err)
	require.Equal(t, containerStartRetryAttempts, starts)
	require.Contains(t, err.Error(), "marked for removal")
}

func TestProvisionPostgresContainerDoesNotRetryHoldError(t *testing.T) {
	wantErr := errors.New("docker daemon unavailable")
	starts := 0
	_, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			starts++
			return &postgres.PostgresContainer{}, nil
		},
		func(context.Context) error { return wantErr },
		func(context.Context) error {
			t.Fatal("verify must not run after hold failure")

			return nil
		},
	)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, 0, starts)
}

func TestProvisionPostgresContainerStartsAfterTransientHoldError(t *testing.T) {
	holds := 0
	starts := 0
	container, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			starts++
			return &postgres.PostgresContainer{}, nil
		},
		func(context.Context) error {
			holds++
			return errors.Join(errSessionReaperUnavailable, context.DeadlineExceeded)
		},
		func(context.Context) error { return nil },
	)

	require.NoError(t, err)
	require.NotNil(t, container)
	require.Equal(t, 1, starts)
	require.Equal(t, 1, holds)
}

func TestShouldFailClosedOnHold(t *testing.T) {
	t.Parallel()

	require.False(t, shouldFailClosedOnHold(nil))
	require.False(t, shouldFailClosedOnHold(errSessionReaperUnavailable))
	require.False(t, shouldFailClosedOnHold(errors.Join(errSessionReaperUnavailable, context.DeadlineExceeded)))
	require.False(t, shouldFailClosedOnHold(context.DeadlineExceeded))
	require.True(t, shouldFailClosedOnHold(errors.New("docker daemon unavailable")))
}

func TestProvisionPostgresContainerRejectsNilStartedContainer(t *testing.T) {
	_, err := provisionPostgresContainer(
		t.Context(),
		"postgres:test",
		func(context.Context, string) (*postgres.PostgresContainer, error) {
			//nolint:nilnil // 컨테이너와 오류를 모두 nil로 돌려주는 start 구현을 거부하는지가 이 테스트의 검증 대상이다.
			return nil, nil
		},
		func(context.Context) error { return nil },
		func(context.Context) error {
			t.Fatal("verify must not run after nil start")

			return nil
		},
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "postgres container start returned nil")
}

func TestPreparePostgresRetryRejectsNilContainer(t *testing.T) {
	err := preparePostgresRetry(t.Context(), nil, errors.New("reaper gone"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unverified postgres container is missing")
}
