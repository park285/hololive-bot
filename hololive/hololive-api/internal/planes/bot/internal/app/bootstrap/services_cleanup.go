package bootstrap

import (
	"io"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type irisCleanupCloser interface {
	Close() error
}

// composeBotInfrastructureCleanup은 bot plane Close에서 한 번만 돈다. 내부 H3 client(alarm-worker·llm-scheduler)를 닫아
// peer에 CONNECTION_CLOSE를 보낸 뒤 Iris client와 infra를 닫는다. 닫지 않으면 peer의 graceful shutdown이 이 연결을
// QUIC idle timeout까지 기다린다. 각 plane의 Close는 aggregate runtime이 모든 plane의 Shutdown(요청 drain)을 끝낸 뒤
// 불리므로 진행 중인 요청을 끊지 않는다(fxapp lifecycleCoordinator.OnStop).
func composeBotInfrastructureCleanup(
	infraCleanup func(),
	irisClient irisCleanupCloser,
	internalClients []io.Closer,
	logger *slog.Logger,
) func() {
	var once sync.Once

	return func() {
		once.Do(func() {
			if err := internalhttp.CloseAll(internalClients...); err != nil && logger != nil {
				logger.Warn("bot_internal_client_close_failed", slog.Any("error", err))
			}

			closeIrisClientForCleanup(irisClient, logger)

			if infraCleanup != nil {
				infraCleanup()
			}
		})
	}
}

func closeIrisClientForCleanup(irisClient irisCleanupCloser, logger *slog.Logger) {
	if irisClient == nil {
		return
	}

	if err := irisClient.Close(); err != nil && logger != nil {
		logger.Warn("iris_client_close_failed", slog.Any("error", err))
	}
}
