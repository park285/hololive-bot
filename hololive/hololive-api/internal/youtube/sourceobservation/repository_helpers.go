package sourceobservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func newLeaseToken() (string, error) {
	var token [32]byte

	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(token[:]), nil
}

func boundedErrorDetail(value string) string {
	if len(value) <= maxErrorTextBytes {
		return value
	}

	return value[:maxErrorTextBytes]
}

// claim·정본 행보다 먼저 직렬화한다. DB 기본 격리 수준과 직렬화 충돌은 그대로 유지한다.
func lockLiveRuntimeState(ctx context.Context, tx dbx.Tx) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_live_runtime_lock.sql")); err != nil {
		return fmt.Errorf("lock youtube live runtime state: %w", err)
	}

	return nil
}
