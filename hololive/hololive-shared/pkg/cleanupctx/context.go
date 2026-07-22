// Package cleanupctx는 값을 보존하면서 취소된 요청에서 분리한 cleanup context를 만든다.
package cleanupctx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultTimeout은 분리된 cleanup 작업의 기본 제한 시간이다.
const DefaultTimeout = 5 * time.Second

// ErrNilDone은 Wait에 완료 channel이 전달되지 않았음을 나타낸다.
var ErrNilDone = errors.New("cleanup done channel is nil")

// WithTimeout은 parent의 값을 보존하고 취소와 deadline을 분리한 cleanup context를 반환한다.
// timeout이 양수가 아니면 DefaultTimeout을 사용한다.
func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

// Wait는 분리되고 제한된 cleanup context로 done을 기다린다.
func Wait(parent context.Context, timeout time.Duration, done <-chan struct{}) error {
	if done == nil {
		return ErrNilDone
	}
	ctx, cancel := WithTimeout(parent, timeout)
	defer cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for cleanup: %w", ctx.Err())
	}
}
