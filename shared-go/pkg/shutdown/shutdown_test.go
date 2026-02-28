package shutdown

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGracefulShutdown(t *testing.T) {
	t.Run("cleanup success", func(t *testing.T) {
		ctx := context.Background()
		timeout := 100 * time.Millisecond

		// 시그널을 수동으로 보내기 위한 작업
		go func() {
			time.Sleep(50 * time.Millisecond)
			process, _ := os.FindProcess(os.Getpid())
			_ = process.Signal(syscall.SIGINT)
		}()

		err := GracefulShutdown(ctx, timeout, func() error {
			return nil
		})

		assert.NoError(t, err)
	})

	t.Run("cleanup failure", func(t *testing.T) {
		ctx := context.Background()
		timeout := 100 * time.Millisecond
		expectedErr := errors.New("cleanup failed")

		go func() {
			time.Sleep(50 * time.Millisecond)
			process, _ := os.FindProcess(os.Getpid())
			_ = process.Signal(syscall.SIGINT)
		}()

		err := GracefulShutdown(ctx, timeout, func() error {
			return expectedErr
		})

		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("timeout", func(t *testing.T) {
		ctx := context.Background()
		timeout := 50 * time.Millisecond

		go func() {
			time.Sleep(50 * time.Millisecond)
			process, _ := os.FindProcess(os.Getpid())
			_ = process.Signal(syscall.SIGINT)
		}()

		err := GracefulShutdown(ctx, timeout, func() error {
			time.Sleep(200 * time.Millisecond)
			return nil
		})

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
