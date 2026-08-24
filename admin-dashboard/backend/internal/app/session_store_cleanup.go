package app

import (
	"context"
	"fmt"

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

// Delete는 client 연결이 끊겨도 server-side session 제거를 제한 시간 동안 시도한다.
func (s cleanupSessionStore) Delete(ctx context.Context, id string) error {
	cleanupCtx, cancel := cleanupctx.WithTimeout(ctx, cleanupctx.DefaultTimeout)
	defer cancel()

	if err := s.sessionStore.Delete(cleanupCtx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	return nil
}
