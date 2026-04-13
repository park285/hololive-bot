package poller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingPublishedAtResolverPoller_DelegatesToRunOnce(t *testing.T) {
	t.Parallel()

	resolverPoller := NewPendingPublishedAtResolverPoller(&PendingPublishedAtResolver{})
	require.NotNil(t, resolverPoller)

	assert.Equal(t, "pending_published_at_resolver", resolverPoller.Name())

	err := resolverPoller.Poll(context.Background(), "ignored-channel")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db is nil")
}
