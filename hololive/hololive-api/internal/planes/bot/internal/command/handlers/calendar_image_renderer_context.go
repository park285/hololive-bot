package handlers

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// CalendarImageRendererContext is an optional capability. Production renderers
// implement it so remote photo work inherits the command lifetime, while legacy
// test and extension renderers remain source-compatible with CalendarImageRenderer.
type CalendarImageRendererContext interface {
	RenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error)
}
