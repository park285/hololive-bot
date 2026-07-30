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

package transport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReplyOutbox struct {
	entries []*ReplyOutboxEntry
}

func (r *recordingReplyOutbox) RecordReply(_ context.Context, entry *ReplyOutboxEntry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func TestReplyOutboxWriterInjection(t *testing.T) {
	t.Parallel()

	t.Run("no writer is configured by default", func(t *testing.T) {
		assert.Nil(t, NewCommandTransport(&stubBotClient{}, nil).replyOutboxWriter())
	})

	t.Run("the option installs the writer", func(t *testing.T) {
		writer := &recordingReplyOutbox{}
		transport := NewCommandTransport(&stubBotClient{}, nil, WithReplyOutboxWriter(writer))

		assert.Same(t, writer, transport.replyOutboxWriter())
	})

	t.Run("an installed writer stays out of the send path", func(t *testing.T) {
		writer := &recordingReplyOutbox{}
		transport := NewCommandTransport(&stubBotClient{}, nil, WithReplyOutboxWriter(writer))
		ctx := WithReplyIdentity(context.Background(), "message:m-1")

		require.NoError(t, transport.SendMessage(ctx, "room-1", "hello"))
		assert.Empty(t, writer.entries, "outbox 배선은 후속 작업 소유다")
	})
}

func TestExportedReplyClientRequestIDMatchesTheSentID(t *testing.T) {
	t.Parallel()

	const identity = "message:m-1"

	client := &stubBotClient{}
	ctx := WithReplyIdentity(context.Background(), identity)
	require.NoError(t, NewCommandTransport(client, nil).SendMessage(ctx, "room-1", "hello"))

	sentID, _ := capturedSendOptions(t, client.lastOpts)
	assert.Equal(t, ReplyClientRequestID(identity, 0), sentID)
	assert.Equal(t, "hololive:v1:"+identity+":"+ReplyPhase+":0", sentID)
}

func TestExportedReplyClientRequestIDIsDeterministic(t *testing.T) {
	t.Parallel()

	const identity = "message:m-1"

	for ordinal := range uint64(4) {
		assert.Equal(t, ReplyClientRequestID(identity, ordinal), ReplyClientRequestID(identity, ordinal))
		assert.Equal(t, replyClientRequestID(identity, ordinal), ReplyClientRequestID(identity, ordinal))
	}
}
