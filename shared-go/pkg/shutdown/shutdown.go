package shutdown

import (
	"context"
	"os/signal"
	"syscall"
	"time"
)

// GracefulShutdown은 SIGINT 또는 SIGTERM 시그널을 대기하고, 시그널 수신 시 cleanup 함수를 실행합니다.
// cleanup 실행 중 timeout이 경과하면 context.DeadlineExceeded 에러를 반환합니다.
func GracefulShutdown(ctx context.Context, timeout time.Duration, cleanup func() error) error {
	// SIGINT, SIGTERM 시그널을 대기하는 context 생성
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 시그널이 올 때까지 대기
	<-sigCtx.Done()

	// cleanup을 위한 타임아웃이 설정된 context 생성
	// 부모 ctx가 이미 취소되었을 수 있으므로 context.Background()를 기반으로 생성
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cleanup()
	}()

	select {
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	case err := <-errCh:
		return err
	}
}
