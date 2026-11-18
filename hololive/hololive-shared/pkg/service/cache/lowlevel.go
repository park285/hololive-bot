package cache

import (
	"context"

	"github.com/valkey-io/valkey-go"
)

type LowLevelCache interface {
	GetClient() valkey.Client
	DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult
	B() valkey.Builder
}
