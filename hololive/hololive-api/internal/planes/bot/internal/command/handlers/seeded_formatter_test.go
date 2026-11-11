package handlers

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

// newSeededTestFormatter는 시드 message_strings와 템플릿을 같은 DB에서 읽는 운영 구성의 formatter를 만든다.
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
