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

// lockLiveRuntimeState는 관측 writer의 shared 전역 잠금을 claim·정본 행보다 먼저 잡는다. 증거 retention·수동 영수증·
// head 복구는 같은 key를 exclusive로 잡으므로 서로 배제되고, 관측 writer끼리는 원본 행과 영상 잠금으로만 직렬화된다.
// 잠금 경합의 40001·40P01·55P03은 트랜잭션 안에서 재시도하지 않고 관측 재시도에 맡긴다.
func lockLiveRuntimeState(ctx context.Context, tx dbx.Tx) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_live_runtime_lock.sql")); err != nil {
		return fmt.Errorf("lock youtube live runtime state: %w", err)
	}

	return nil
}

// refreshLiveRuntimeState는 이 트랜잭션이 바꿨을 수 있는 영상 key로 youtube_live_runtime_state를 한 번 재계산한다.
// 원본 쓰기를 모두 마친 뒤 호출한다. 고정 snapshot 트랜잭션의 호출은 DB 함수가 거부하며, 빈 key는 왕복하지 않는다.
func refreshLiveRuntimeState(ctx context.Context, tx dbx.Tx, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, mustSQL("repository_live_runtime_refresh.sql"), videoIDs); err != nil {
		return fmt.Errorf("refresh youtube live runtime state: %w", err)
	}

	return nil
}
