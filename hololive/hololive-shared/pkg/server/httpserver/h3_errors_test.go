package httpserver

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type listenerErrorLog struct {
	recorded chan struct{}
}

func (l listenerErrorLog) Write(p []byte) (int, error) {
	l.recorded <- struct{}{}

	return len(p), nil
}

func TestRuntimeServersReportOneTerminalError(t *testing.T) {
	t.Parallel()

	recorded := make(chan struct{}, 3)
	logger := slog.New(slog.NewTextHandler(listenerErrorLog{recorded: recorded}, nil))
	servers := &RuntimeHTTPServers{
		H3:      &http3.Server{Addr: "invalid"},
		Metrics: &http.Server{Addr: "invalid", ReadHeaderTimeout: serverReadHeaderTimeout},
		Pprof:   &http.Server{Addr: "invalid", ReadHeaderTimeout: serverReadHeaderTimeout},
	}
	errors := make(chan error, 1)
	servers.Start(logger, errors)

	for range 3 {
		select {
		case <-recorded:
		case <-time.After(time.Second):
			t.Fatal("listener failure did not finish reporting")
		}
	}

	select {
	case err := <-errors:
		if err == nil {
			t.Fatal("nil terminal error")
		}
	case <-time.After(time.Second):
		t.Fatal("missing terminal listener error")
	}

	select {
	case err := <-errors:
		t.Fatalf("extra terminal listener error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}
