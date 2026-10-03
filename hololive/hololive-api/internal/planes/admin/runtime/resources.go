package adminruntime

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

// adminRuntimeResources는 획득 즉시 등록한 자원을 실패 rollback과 정상 종료에서 같은 owner로 해제한다.
type adminRuntimeResources struct {
	once      sync.Once
	infra     *sharedmodules.InfraModule
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
		if r.infra != nil && r.infra.StopMemberCache != nil {
			r.infra.StopMemberCache()
		}

		if r.holodex != nil {
			r.holodex.Stop()
		}

		if err := internalhttp.CloseAll(r.rooms, r.collector, r.trigger, r.alarm); err != nil {
			r.closeErr = fmt.Errorf("close admin internal clients: %w", err)

			if r.logger != nil {
				r.logger.Warn("admin_internal_client_close_failed", slog.Any("error", err))
			}
		}

		if r.infra != nil && r.infra.Cleanup != nil {
			r.infra.Cleanup()
		}
	})

	return r.closeErr
}
