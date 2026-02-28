package processinglock

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"
)

func newTestService(t *testing.T, keyPrefix string, ttl time.Duration) (*Service, valkey.Client, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run failed: %v", err)
	}

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{mr.Addr()},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	if err != nil {
		mr.Close()
		t.Fatalf("valkey client create failed: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(client, logger, func(chatID string) string {
		return keyPrefix + chatID
	}, ttl)

	return svc, client, mr
}

func TestService_Start_MutualExclusion(t *testing.T) {
	svc, client, mr := newTestService(t, "test:processing:", 10*time.Second)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()
	chatID := "room1"

	if err := svc.Start(ctx, chatID); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := svc.Start(ctx, chatID); !errors.Is(err, ErrAlreadyProcessing) {
		t.Fatalf("expected ErrAlreadyProcessing, got: %v", err)
	}

	ok, err := svc.IsProcessing(ctx, chatID)
	if err != nil {
		t.Fatalf("is processing failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected processing true")
	}

	if err := svc.Finish(ctx, chatID); err != nil {
		t.Fatalf("finish failed: %v", err)
	}

	ok, err = svc.IsProcessing(ctx, chatID)
	if err != nil {
		t.Fatalf("is processing failed: %v", err)
	}
	if ok {
		t.Fatalf("expected processing false")
	}

	if err := svc.Start(ctx, chatID); err != nil {
		t.Fatalf("start after finish failed: %v", err)
	}
}

func TestService_TTLExpiry(t *testing.T) {
	svc, client, mr := newTestService(t, "test:processing:", 2*time.Second)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()
	chatID := "room1"

	if err := svc.Start(ctx, chatID); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	mr.FastForward(3 * time.Second)

	ok, err := svc.IsProcessing(ctx, chatID)
	if err != nil {
		t.Fatalf("is processing failed: %v", err)
	}
	if ok {
		t.Fatalf("expected processing false after ttl")
	}

	err = svc.Start(ctx, chatID)
	if err != nil {
		t.Fatalf("start after TTL should succeed: %v", err)
	}
}

func TestService_DifferentChatIDs(t *testing.T) {
	svc, client, mr := newTestService(t, "test:processing:", 10*time.Second)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()

	err := svc.Start(ctx, "chat-A")
	if err != nil {
		t.Fatalf("Start chat-A failed: %v", err)
	}

	err = svc.Start(ctx, "chat-B")
	if err != nil {
		t.Fatalf("Start chat-B should succeed: %v", err)
	}
}

func TestService_ListLocks(t *testing.T) {
	svc, client, mr := newTestService(t, "test:lock:", 10*time.Second)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()

	_ = svc.Start(ctx, "room1")
	_ = svc.Start(ctx, "room2")
	_ = svc.Start(ctx, "room3")

	keys, err := svc.ListLocks(ctx, "test:lock:*")
	if err != nil {
		t.Fatalf("ListLocks failed: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got: %d", len(keys))
	}
}

func TestService_Finish(t *testing.T) {
	svc, client, mr := newTestService(t, "test:processing:", 10*time.Second)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()
	chatID := "test-chat-456"

	_ = svc.Start(ctx, chatID)

	err := svc.Finish(ctx, chatID)
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	err = svc.Start(ctx, chatID)
	if err != nil {
		t.Fatalf("Start after Finish should succeed: %v", err)
	}
}
