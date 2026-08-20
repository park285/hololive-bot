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
		context.Background(),
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
		context.Background(),
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
		context.Background(),
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
