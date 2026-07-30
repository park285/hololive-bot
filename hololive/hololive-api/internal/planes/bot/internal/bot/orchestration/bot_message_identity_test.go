// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package orchestration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/webhook"
)

func TestMessageReplyIdentityFallbackChain(t *testing.T) {
	t.Parallel()

	sourceLogID := int64(42)

	cases := []struct {
		name    string
		message *webhook.Message
		want    string
	}{
		{"message id wins", &webhook.Message{JSON: &webhook.MessageJSON{
			MessageID: " m-1 ", ChatLogID: "c-1", SourceLogID: &sourceLogID,
		}}, "message:m-1"},
		{"chat log id is the first fallback", &webhook.Message{JSON: &webhook.MessageJSON{
			ChatLogID: "c-1", SourceLogID: &sourceLogID,
		}}, "chat-log:c-1"},
		{"source log id is the last fallback", &webhook.Message{JSON: &webhook.MessageJSON{
			SourceLogID: &sourceLogID,
		}}, "source-log:42"},
		{"no identity at all", &webhook.Message{JSON: &webhook.MessageJSON{}}, ""},
		{"no json envelope", &webhook.Message{}, ""},
		{"no message", nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, messageReplyIdentity(tc.message))
		})
	}
}

func TestCommandContextMessageIDTakesPrecedenceOverTheFallbackChain(t *testing.T) {
	t.Parallel()

	message := &webhook.Message{JSON: &webhook.MessageJSON{MessageID: "from-webhook"}}

	t.Run("first-class message id is used", func(t *testing.T) {
		cmdCtx := &domain.CommandContext{Room: "room-1", MessageID: "message:canonical"}

		identity, ok := transport.ReplyIdentityFromContext(commandRequestContext(context.Background(), cmdCtx, message))
		require.True(t, ok)
		assert.Equal(t, "message:canonical", identity)
	})

	t.Run("surrounding whitespace does not create an identity", func(t *testing.T) {
		cmdCtx := &domain.CommandContext{Room: "room-1", MessageID: "   "}

		identity, ok := transport.ReplyIdentityFromContext(commandRequestContext(context.Background(), cmdCtx, message))
		require.True(t, ok)
		assert.Equal(t, "message:from-webhook", identity, "blank field must fall back to the webhook chain")
	})

	t.Run("absent message id falls back to the webhook chain", func(t *testing.T) {
		cmdCtx := &domain.CommandContext{Room: "room-1"}

		identity, ok := transport.ReplyIdentityFromContext(commandRequestContext(context.Background(), cmdCtx, message))
		require.True(t, ok)
		assert.Equal(t, "message:from-webhook", identity)
	})

	t.Run("no identity anywhere leaves the context untouched", func(t *testing.T) {
		cmdCtx := &domain.CommandContext{Room: "room-1"}

		_, ok := transport.ReplyIdentityFromContext(commandRequestContext(context.Background(), cmdCtx, nil))
		assert.False(t, ok)
	})
}

func TestCommandContextMessageIDDrivesTheReplyClientRequestID(t *testing.T) {
	t.Parallel()

	cmdCtx := &domain.CommandContext{Room: "room-1", MessageID: "message:canonical"}
	reqCtx := commandRequestContext(context.Background(), cmdCtx, nil)

	identity, ok := transport.ReplyIdentityFromContext(reqCtx)
	require.True(t, ok)
	assert.Equal(t, "hololive:v1:message:canonical:reply:0", transport.ReplyClientRequestID(identity, 0))
}
