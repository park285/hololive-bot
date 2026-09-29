package bootstrap

import (
	"io"
	"testing"
)

type fakeCleanupCloser struct {
	closed int
}

func (c *fakeCleanupCloser) Close() error {
	c.closed++
	return nil
}

func TestComposeBotInfrastructureCleanupClosesClientsAndInfraOnce(t *testing.T) {
	t.Parallel()

	irisClient := &fakeCleanupCloser{}
	alarmClient := &fakeCleanupCloser{}
	infraClosed := 0
	cleanup := composeBotInfrastructureCleanup(func() {
		infraClosed++
	}, irisClient, []io.Closer{alarmClient, nil}, nil)

	cleanup()
	cleanup()

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
