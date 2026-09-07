package youtubejs

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSocketReadinessWaitsForListenAfterBind(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "helper.sock")

	fd, socketErr := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if socketErr != nil {
		t.Fatal(socketErr)
	}

	t.Cleanup(func() {
		if err := syscall.Close(fd); err != nil {
			t.Errorf("close socket: %v", err)
		}
	})

	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: socketPath}); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}

	helper := &Helper{socketPath: socketPath, waited: make(chan struct{})}
	tick := make(chan time.Time, 1)

	tick <- time.Now()

	ready, err := helper.waitForSocketEvent(t.Context(), tick)
	if err != nil || ready {
		t.Fatalf("bound socket before listen: ready=%t err=%v", ready, err)
	}

	if listenErr := syscall.Listen(fd, 1); listenErr != nil {
		t.Fatal(listenErr)
	}

	tick <- time.Now()

	ready, err = helper.waitForSocketEvent(t.Context(), tick)
	if err != nil || !ready {
		t.Fatalf("listening socket: ready=%t err=%v", ready, err)
	}
}
