package cache

import "context"

type ScriptCache interface {
	CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error)
}
