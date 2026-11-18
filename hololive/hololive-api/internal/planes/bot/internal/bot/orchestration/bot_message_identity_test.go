package orchestration

import (
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestCanonicalReplyIdentityHasNoFallbackChain(t *testing.T) {
	t.Parallel()

	sourceLogID := int64(42)

	cases := []struct {
		name    string
		message *webhook.Message
		want    string
	}{
		{"canonical message id", &webhook.Message{JSON: &webhook.MessageJSON{
			MessageID: " m-1 ", ChatLogID: "c-1", SourceLogID: &sourceLogID,
		}}, "message:m-1"},
		{"chat log id is not an identity", &webhook.Message{JSON: &webhook.MessageJSON{
			ChatLogID: "c-1", SourceLogID: &sourceLogID,
		}}, ""},
		{"source log id is not an identity", &webhook.Message{JSON: &webhook.MessageJSON{
			SourceLogID: &sourceLogID,
		}}, ""},
		{"blank message id", &webhook.Message{JSON: &webhook.MessageJSON{MessageID: "   "}}, ""},
		{"no identity at all", &webhook.Message{JSON: &webhook.MessageJSON{}}, ""},
		{"no json envelope", &webhook.Message{}, ""},
		{"no message", nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, canonicalReplyIdentity(tc.message))
		})
	}
}

func TestCommandRequestContextUsesTheCommandContextMessageID(t *testing.T) {
	t.Parallel()

	cmdCtx := &botcommand.CommandContext{Room: testRoomID, MessageID: "message:canonical"}
	reqCtx := commandRequestContext(t.Context(), cmdCtx)

	identity, ok := transport.ReplyIdentityFromContext(reqCtx)
	require.True(t, ok)
	assert.Equal(t, "message:canonical", identity)
	assert.Equal(t, "hololive:v1:message:canonical:reply:0", transport.ReplyClientRequestID(identity, 0))
}
