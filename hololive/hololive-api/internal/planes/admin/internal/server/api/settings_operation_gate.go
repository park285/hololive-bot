package api

import (
	"context"
	"fmt"
	"sync"
)

// settingsOperationGate는 장수명 admin Handler가 소유한다. 저장과 worker 적용을 같은 순서로 묶고,
// GET이 저장 뒤 적용 전의 서로 다른 snapshot을 조합하지 않게 한다. 대기는 요청의 전체 예산에 포함된다.
type settingsOperationGate struct {
	once     sync.Once
	requests chan struct{}
}

func (g *settingsOperationGate) acquire(ctx context.Context) (func(), error) {
	g.once.Do(func() { g.requests = make(chan struct{}, 1) })

	select {
	case g.requests <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for settings operation: %w", ctx.Err())
	}

	if err := ctx.Err(); err != nil {
		<-g.requests

		return nil, fmt.Errorf("before settings operation: %w", err)
	}

	return func() { <-g.requests }, nil
}

func (h *SettingsHandler) settingsOperationGate() *settingsOperationGate {
	if h.operationGate != nil {
		return h.operationGate
	}

	return &h.operations
}
