package handlers

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type contextCalendarRendererStub struct {
	seenContexts []context.Context
}

func (s *contextCalendarRendererStub) RenderCalendarImageContext(ctx context.Context, _, _ int, _ []domain.CalendarEntry) ([]byte, error) {
	s.seenContexts = append(s.seenContexts, ctx)

	return []byte("png"), nil
}

func TestCalendarCommandRenderCalendarImageUsesContextCapability(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx := context.WithValue(t.Context(), contextKey{}, "request")
	stub := &contextCalendarRendererStub{}
	command := &CalendarCommand{imageRenderer: stub}

	data, err := command.renderCalendarImage(ctx, 6, 2026, nil)
	if err != nil {
		t.Fatalf("renderCalendarImage() error = %v", err)
	}

	if string(data) != "png" {
		t.Fatalf("renderCalendarImage() data = %q", data)
	}

	if len(stub.seenContexts) != 1 {
		t.Fatalf("context-aware renderer call count = %d, want 1", len(stub.seenContexts))
	}

	if seen := stub.seenContexts[0]; seen == nil || seen.Value(contextKey{}) != "request" {
		t.Fatal("caller context was not forwarded to context-aware renderer")
	}
}
