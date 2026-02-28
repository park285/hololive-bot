package bot

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/iris"
)

type fakeIrisClient struct {
	failCount int
	pingCalls int
}

func (f *fakeIrisClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
}
func (f *fakeIrisClient) SendImage(context.Context, string, string) error { return nil }
func (f *fakeIrisClient) GetConfig(context.Context) (*iris.Config, error) { return &iris.Config{}, nil }
func (f *fakeIrisClient) Decrypt(context.Context, string) (string, error) { return "", nil }
func (f *fakeIrisClient) Ping(context.Context) bool {
	f.pingCalls++
	if f.failCount > 0 {
		f.failCount--
		return false
	}
	return true
}

func TestWaitUntilIrisReady_SucceedsAfterRetry(t *testing.T) {
	t.Parallel()

	client := &fakeIrisClient{failCount: 2}
	b := &Bot{
		irisClient: client,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := b.waitUntilIrisReady(context.Background(), 300*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("waitUntilIrisReady failed: %v", err)
	}
	if client.pingCalls < 3 {
		t.Fatalf("expected at least 3 ping attempts, got %d", client.pingCalls)
	}
}

func TestWaitUntilIrisReady_TimesOut(t *testing.T) {
	t.Parallel()

	client := &fakeIrisClient{failCount: 1000}
	b := &Bot{
		irisClient: client,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := b.waitUntilIrisReady(context.Background(), 70*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}
