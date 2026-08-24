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
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type replyIdentityProbeCommand struct {
	calls atomic.Int64
}

func (c *replyIdentityProbeCommand) Name() string        { return testHelpCommandName }
func (c *replyIdentityProbeCommand) Description() string { return testHelpCommandName }

func (c *replyIdentityProbeCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	c.calls.Add(1)

	return nil
}

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

func TestProcessMessageRefusesToProcessWithoutCanonicalMessageID(t *testing.T) {
	t.Parallel()

	executed := &replyIdentityProbeCommand{}
	registry := command.NewRegistry()
	registry.Register(executed)

	var logs bytes.Buffer

	bot := &Bot{
		logger:          slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		commandRegistry: registry,
		messageAdapter:  messaging.NewMessageAdapter("!", ""),
		formatter:       formatter.NewResponseFormatter("!", nil),
	}

	sender := testSenderName
	require.NoError(t, bot.ProcessMessage(t.Context(), &webhook.Message{
		Msg:    "!help",
		Room:   "12345",
		Sender: &sender,
		JSON:   &webhook.MessageJSON{UserID: testUserID, ChatID: "12345", ChatLogID: "c-1"},
	}))

	assert.Zero(t, executed.calls.Load(), "command must not run without a canonical message id")
	assert.Contains(t, logs.String(), EventBotReplyIdentityMissing)
}

func TestCommandRequestContextUsesTheCommandContextMessageID(t *testing.T) {
	t.Parallel()

	cmdCtx := &domain.CommandContext{Room: testRoomID, MessageID: "message:canonical"}
	reqCtx := commandRequestContext(t.Context(), cmdCtx)

	identity, ok := transport.ReplyIdentityFromContext(reqCtx)
	require.True(t, ok)
	assert.Equal(t, "message:canonical", identity)
	assert.Equal(t, "hololive:v1:message:canonical:reply:0", transport.ReplyClientRequestID(identity, 0))
}
