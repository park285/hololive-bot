// Package cleanupctx는 값을 보존하면서 취소된 요청에서 분리한 cleanup context를 만든다.
package cleanupctx

import (
	"context"
	"time"
)

// DefaultTimeout은 분리된 cleanup 작업의 기본 제한 시간이다.
const DefaultTimeout = 5 * time.Second

// WithTimeout은 parent의 값을 보존하고 취소와 deadline을 분리한 cleanup context를 반환한다.
// 지정한 timeout이 양수가 아니면 DefaultTimeout을 사용한다.
func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
