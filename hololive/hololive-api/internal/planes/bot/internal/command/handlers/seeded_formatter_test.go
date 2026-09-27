package handlers

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// newSeededTestFormatter는 시드 message_strings를 적재한 formatter를 만든다. 코드 대체 문구가 없으므로
// renderer가 없는 formatter의 응답도 DB 정본 문구(command_processing_failed)에서만 나온다.
func newSeededTestFormatter(t *testing.T) *formatter.ResponseFormatter {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	return formatter.NewResponseFormatter("!", nil, formatter.WithMessageStrings(store))
}
