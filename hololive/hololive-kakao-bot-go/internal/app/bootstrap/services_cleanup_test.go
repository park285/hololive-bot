package bootstrap

import "testing"

type fakeIrisCleanupClient struct {
	closed int
}

func (c *fakeIrisCleanupClient) Close() error {
	c.closed++
	return nil
}

func TestComposeBotInfrastructureCleanupClosesIrisAndInfraOnce(t *testing.T) {
	t.Parallel()

	irisClient := &fakeIrisCleanupClient{}
	infraClosed := 0
	cleanup := composeBotInfrastructureCleanup(func() {
		infraClosed++
	}, irisClient, nil)

	cleanup()
	cleanup()

	if irisClient.closed != 1 {
		t.Fatalf("iris client close count = %d, want 1", irisClient.closed)
	}
	if infraClosed != 1 {
		t.Fatalf("infra close count = %d, want 1", infraClosed)
	}
}

func TestCloseIrisClientForCleanupIgnoresNonCloser(t *testing.T) {
	t.Parallel()

	closeIrisClientForCleanup(struct{}{}, nil)
}
