package adminruntime

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

// adminRuntimeResources는 획득 즉시 등록한 자원을 실패 rollback과 정상 종료에서 같은 owner로 해제한다.
type adminRuntimeResources struct {
	once      sync.Once
	holodex   interface{ Stop() }
	alarm     io.Closer
	trigger   io.Closer
	collector io.Closer
	rooms     io.Closer
	logger    *slog.Logger
	closeErr  error
}

func (r *adminRuntimeResources) Close() error {
	r.once.Do(func() {
		if r.holodex != nil {
			r.holodex.Stop()
		}

		if err := internalhttp.CloseAll(r.rooms, r.collector, r.trigger, r.alarm); err != nil {
			r.closeErr = fmt.Errorf("close admin internal clients: %w", err)

			if r.logger != nil {
				r.logger.Warn("admin_internal_client_close_failed", slog.Any("error", err))
			}
		}
	})

	return r.closeErr
}
