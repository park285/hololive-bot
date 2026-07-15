package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type contextCalendarRendererStub struct {
	seenContext context.Context
}

func (*contextCalendarRendererStub) RenderCalendarImage(int, int, []domain.CalendarEntry) ([]byte, error) {
	return nil, errors.New("legacy render path must not be used when context capability is available")
}

func (s *contextCalendarRendererStub) RenderCalendarImageContext(ctx context.Context, _, _ int, _ []domain.CalendarEntry) ([]byte, error) {
	s.seenContext = ctx
	return []byte("png"), nil
}

func TestCalendarCommandRenderCalendarImageUsesContextCapability(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	stub := &contextCalendarRendererStub{}
	command := &CalendarCommand{imageRenderer: stub}

	data, err := command.renderCalendarImage(ctx, 6, 2026, nil)
	if err != nil {
		t.Fatalf("renderCalendarImage() error = %v", err)
	}
	if string(data) != "png" {
		t.Fatalf("renderCalendarImage() data = %q", data)
	}
	if stub.seenContext == nil || stub.seenContext.Value(contextKey{}) != "request" {
		t.Fatal("caller context was not forwarded to context-aware renderer")
	}
}
