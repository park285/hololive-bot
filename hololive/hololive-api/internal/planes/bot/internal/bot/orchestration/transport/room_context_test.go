package transport

import (
	"context"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/domain"
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

func TestReplyFormatDecisionFixesRouteForRequest(t *testing.T) {
	t.Parallel()

	const body = "**안내** 본문"

	for _, openAtDecision := range []bool{false, true} {
		rooms := staticRoomChat{"9": openAtDecision}
		writer := &recordingReplyOutbox{}
		tr := NewCommandTransport(writer, nil, WithMarkdownReplies(true), WithRoomChatLookup(rooms))
		ctx := tr.WithReplyFormat(WithReplyIdentity(t.Context(), "message:m-1"), "9")

		format, ok := messageformatter.ReplyFormatFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, openAtDecision, format == domain.TemplateFormatRichText)

		rooms["9"] = !openAtDecision

		require.NoError(t, tr.SendMessage(ctx, "9", body))
		require.Len(t, writer.entries, 1)

		var frozen StoredReply

		require.NoError(t, jsonv2.Unmarshal([]byte(writer.entries[0].Payload), &frozen))

		if openAtDecision {
			require.Equal(t, StoredReplyKindRichText, frozen.Kind)
			require.Equal(t, body, frozen.Message)
		} else {
			require.Equal(t, StoredReplyKindText, frozen.Kind)
			require.Equal(t, kakaoformat.Render(body), frozen.Message)
		}
	}
}
