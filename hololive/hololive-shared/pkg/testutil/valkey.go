package testutil

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testutil/cacheclient"
)

func NewTestValkeyClient(t *testing.T) (valkey.Client, *miniredis.Miniredis) {
	t.Helper()

	client, mini := cacheclient.NewValkeyClientWithMini(t)
	t.Cleanup(func() { client.Close() })

	return client, mini
}
