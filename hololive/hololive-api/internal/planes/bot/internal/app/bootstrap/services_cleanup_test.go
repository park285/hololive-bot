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

func TestBotInfrastructureOwnerClosesClientsAndInfraOnce(t *testing.T) {
	t.Parallel()

	irisClient := &fakeCleanupCloser{}
	alarmClient := &fakeCleanupCloser{}
	infraClosed := 0
	owner := &botInfrastructureOwner{
		infraCleanup:    func() { infraClosed++ },
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

	if infraClosed != 1 {
		t.Fatalf("infra close count = %d, want 1", infraClosed)
	}
}
