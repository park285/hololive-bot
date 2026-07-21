package app

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/cleanupctx"
)

type cleanupSessionStore struct {
	sessionStore
}

func newCleanupSessionStore(store sessionStore) sessionStore {
	if store == nil {
		return nil
	}
	return cleanupSessionStore{sessionStore: store}
}

// Delete is a revoke/cleanup operation: once requested, it must get a bounded
// chance to remove the server-side session even if the client disconnects.
func (s cleanupSessionStore) Delete(ctx context.Context, id string) error {
	cleanupCtx, cancel := cleanupctx.WithTimeout(ctx, cleanupctx.DefaultTimeout)
	defer cancel()
	return s.sessionStore.Delete(cleanupCtx, id)
}
