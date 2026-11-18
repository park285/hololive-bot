package handlers

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

func newSeededTestFormatter(t *testing.T) *formatter.ResponseFormatter {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)

	store := messagestrings.NewStore(pool, logger)
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	return formatter.NewResponseFormatter("!", serviceTemplate.NewRenderer(pool, logger), formatter.WithMessageStrings(store))
}
