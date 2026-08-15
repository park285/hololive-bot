package transport

import (
	"context"
	"testing"
)

type staticRoomChat map[string]bool

func (s staticRoomChat) OpenChat(_ context.Context, roomID string) bool {
	return s[roomID]
}

func TestMarkdownForRoom(t *testing.T) {
	t.Parallel()

	ctx := WithRoomChat(t.Context(), "OM", "")
	if !markdownForRoom(ctx, "1", true, nil) {
		t.Fatal("open chat context should keep markdown")
	}

	ctx = WithRoomChat(t.Context(), "DirectChat", "")
	if markdownForRoom(ctx, "1", true, nil) {
		t.Fatal("direct chat context should drop markdown")
	}

	if markdownForRoom(t.Context(), "1", true, nil) {
		t.Fatal("unknown room without lookup should be plain")
	}

	if !markdownForRoom(t.Context(), "9", true, staticRoomChat{"9": true}) {
		t.Fatal("lookup open room should keep markdown")
	}

	if markdownForRoom(t.Context(), "9", false, staticRoomChat{"9": true}) {
		t.Fatal("disabled flag should force plain")
	}
}
