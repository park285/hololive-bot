package handlers

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type contextCalendarRendererStub struct {
	seenContexts []context.Context
}

func (s *contextCalendarRendererStub) RenderCalendarImagesContext(ctx context.Context, _, _ int, _ []domain.CalendarEntry) ([][]byte, error) {
	s.seenContexts = append(s.seenContexts, ctx)

	return [][]byte{[]byte("png")}, nil
}

func TestCalendarCommandRenderCalendarImagesUsesContextCapability(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx := context.WithValue(t.Context(), contextKey{}, "request")
	stub := &contextCalendarRendererStub{}
	command := &CalendarCommand{imageRenderer: stub}

	pages, err := command.renderCalendarImages(ctx, 6, 2026, nil)
	if err != nil {
		t.Fatalf("renderCalendarImages() error = %v", err)
	}

	if len(pages) != 1 || string(pages[0]) != "png" {
		t.Fatalf("renderCalendarImages() pages = %q", pages)
	}

	if len(stub.seenContexts) != 1 {
		t.Fatalf("context-aware renderer call count = %d, want 1", len(stub.seenContexts))
	}

	if seen := stub.seenContexts[0]; seen == nil || seen.Value(contextKey{}) != "request" {
		t.Fatal("caller context was not forwarded to context-aware renderer")
	}
}
