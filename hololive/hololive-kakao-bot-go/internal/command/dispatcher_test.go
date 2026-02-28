package command

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubCommand struct {
	name     string
	executed int
}

func (s *stubCommand) Name() string { return s.name }

func (s *stubCommand) Description() string { return "stub" }

func (s *stubCommand) Execute(_ context.Context, _ *domain.CommandContext, _ map[string]any) error {
	s.executed++
	return nil
}

func TestSequentialDispatcherPublishRequiresConfiguration(t *testing.T) {
	ctx := context.Background()

	var nilDispatcher *sequentialDispatcher
	if _, err := nilDispatcher.Publish(ctx, nil); err == nil {
		t.Fatal("expected error for nil dispatcher")
	}

	noRegistry := &sequentialDispatcher{normalize: func(_ domain.CommandType, p map[string]any) (string, map[string]any) {
		return "help", p
	}}
	if _, err := noRegistry.Publish(ctx, nil); err == nil {
		t.Fatal("expected error when registry is nil")
	}

	noNormalize := &sequentialDispatcher{registry: NewRegistry()}
	if _, err := noNormalize.Publish(ctx, nil); err == nil {
		t.Fatal("expected error when normalize func is nil")
	}
}

func TestSequentialDispatcherPublishExecutesEvents(t *testing.T) {
	registry := NewRegistry()
	cmd := &stubCommand{name: "help"}
	registry.Register(cmd)

	dispatcher := NewSequentialDispatcher(
		registry,
		func(_ domain.CommandType, p map[string]any) (string, map[string]any) {
			return "help", p
		},
	)

	executed, err := dispatcher.Publish(context.Background(), &domain.CommandContext{}, Event{
		Type:   domain.CommandHelp,
		Params: map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}
	if cmd.executed != 1 {
		t.Fatalf("command executed = %d, want 1", cmd.executed)
	}
}
