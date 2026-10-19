package bootstrap

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeCleanupCloser struct {
	closed int
}

func (c *fakeCleanupCloser) Close() error {
	c.closed++
	return nil
}

func TestBotInfrastructureOwnerClosesClientsOnce(t *testing.T) {
	t.Parallel()

	irisClient := &fakeCleanupCloser{}
	alarmClient := &fakeCleanupCloser{}
	owner := &botInfrastructureOwner{
		irisClient:      irisClient,
		internalClients: []io.Closer{alarmClient, nil},
	}

	require.NoError(t, owner.Close())
	require.NoError(t, owner.Close())

	if irisClient.closed != 1 {
		t.Fatalf("iris client close count = %d, want 1", irisClient.closed)
	}

	if alarmClient.closed != 1 {
		t.Fatalf("internal client close count = %d, want 1", alarmClient.closed)
	}
}
