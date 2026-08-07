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
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReplyOutbox struct {
	mu      sync.Mutex
	entries []*ReplyOutboxEntry
}

func (r *recordingReplyOutbox) snapshot() []*ReplyOutboxEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*ReplyOutboxEntry(nil), r.entries...)
}

type failingReplyOutbox struct{ err error }

type storedReplyReissueClient struct {
	stubBotClient
	acceptErrors  []error
	messages      []string
	optsByAttempt [][]iris.SendOption
	acceptedCalls int
}

func (c *storedReplyReissueClient) SendMessageAccepted(_ context.Context, _, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.acceptedCalls++
	c.messages = append(c.messages, message)
	c.optsByAttempt = append(c.optsByAttempt, append([]iris.SendOption(nil), opts...))
	if len(c.acceptErrors) >= c.acceptedCalls && c.acceptErrors[c.acceptedCalls-1] != nil {
		return nil, c.acceptErrors[c.acceptedCalls-1]
	}
	return &iris.ReplyAcceptedResponse{RequestID: "iris-reissued"}, nil
}

func (w failingReplyOutbox) RecordReply(context.Context, *ReplyOutboxEntry) error { return w.err }

func (r *recordingReplyOutbox) RecordReply(_ context.Context, entry *ReplyOutboxEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

	t.Run("an installed writer owns the send path", func(t *testing.T) {
		writer := &recordingReplyOutbox{}
		transport := NewCommandTransport(&stubBotClient{}, nil, WithReplyOutboxWriter(writer))
		ctx := WithReplyIdentity(context.Background(), "message:m-1")

		require.NoError(t, transport.SendMessage(ctx, "room-1", "hello"))
		require.Len(t, writer.entries, 1)
		assert.Equal(t, "room-1", writer.entries[0].Room)
		assert.Contains(t, writer.entries[0].Payload, `"message":"hello"`)
	})

	t.Run("staging failures carry typed uncertainty", func(t *testing.T) {
		transport := NewCommandTransport(&stubBotClient{}, nil,
			WithReplyOutboxWriter(failingReplyOutbox{err: errors.New("postgres unavailable")}))
		ctx := WithReplyIdentity(context.Background(), "message:m-stage-fail")

		err := transport.SendMessage(ctx, "room-1", "hello")
		require.ErrorIs(t, err, ErrReplyStagingFailed)
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

func TestReplyOutboxPersistsImageContentType(t *testing.T) {
	t.Parallel()

	writer := &recordingReplyOutbox{}
	transport := NewCommandTransport(&stubBotClient{}, nil, WithReplyOutboxWriter(writer))
	ctx := WithReplyIdentity(context.Background(), "message:m-image")
	ctx = WithImageContentType(ctx, "video/mp4")

	require.NoError(t, transport.SendImage(ctx, "room-1", []byte("media")))
	require.Len(t, writer.entries, 1)
	assert.Contains(t, writer.entries[0].Payload, `"image_content_type":"video/mp4"`)
}

func TestDispatchStoredReplyReissuesFailedConflictWithStoredPayload(t *testing.T) {
	t.Parallel()

	failed := &iris.HTTPError{
		StatusCode: http.StatusConflict,
		URL:        "https://iris/reply",
		Body:       `{"code":"CLIENT_REQUEST_ID_FAILED"}`,
	}
	client := &storedReplyReissueClient{acceptErrors: []error{failed, nil}}
	payload := []byte(`{"kind":"text","message":"persisted payload"}`)
	originalPayload := append([]byte(nil), payload...)
	acceptedRequestID := ""

	err := DispatchStoredReply(t.Context(), client, "room-1", payload, "hololive:v1:message:m-1:reply:0", func(_ context.Context, requestID string) error {
		acceptedRequestID = requestID
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "iris-reissued", acceptedRequestID)
	assert.Equal(t, originalPayload, payload)
	assert.Equal(t, []string{"persisted payload", "persisted payload"}, client.messages)
	require.Len(t, client.optsByAttempt, 2)
	first, _ := capturedSendOptions(t, client.optsByAttempt[0])
	second, _ := capturedSendOptions(t, client.optsByAttempt[1])
	assert.Equal(t, "hololive:v1:message:m-1:reply:0", first)
	assert.Equal(t, "hololive:v1:message:m-1:reply:0:r1", second)
}

func TestStoredReplyKindsStayDispatchable(t *testing.T) {
	t.Parallel()

	for _, kind := range []StoredReplyKind{
		StoredReplyKindText,
		StoredReplyKindMarkdown,
		StoredReplyKindImage,
		StoredReplyKindImages,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			client := &stubBotClient{}
			reply := StoredReply{
				Kind: kind, Message: "message", Image: []byte("image"), Images: [][]byte{[]byte("image")},
			}
			accepted, err := postStoredReply(t.Context(), client, "room-1", &reply, nil)
			require.NoError(t, err)
			require.NotNil(t, accepted)
		})
	}
}

func TestExportedReplyClientRequestIDIsDeterministic(t *testing.T) {
	t.Parallel()

	const identity = "message:m-1"

	for ordinal := range uint64(4) {
		assert.Equal(t, ReplyClientRequestID(identity, ordinal), ReplyClientRequestID(identity, ordinal))
		assert.Equal(t, replyClientRequestID(identity, ordinal), ReplyClientRequestID(identity, ordinal))
	}
}

func TestConcurrentSendsRecordDistinctOrdinalsMatchingClientRequestIDs(t *testing.T) {
	t.Parallel()

	const identity = "message:m-race"
	writer := &recordingReplyOutbox{}
	transport := NewCommandTransport(&stubBotClient{}, nil, WithReplyOutboxWriter(writer))
	ctx := WithReplyIdentity(context.Background(), identity)

	const sends = 8
	var wg sync.WaitGroup
	sendErrs := make([]error, sends)
	for i := range sends {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			sendErrs[slot] = transport.SendMessage(ctx, "room-race", "hello")
		}(i)
	}
	wg.Wait()
	for i, err := range sendErrs {
		require.NoErrorf(t, err, "send %d", i)
	}

	entries := writer.snapshot()
	require.Len(t, entries, sends)
	seen := make(map[uint64]struct{}, sends)
	for _, entry := range entries {
		if _, dup := seen[entry.Ordinal]; dup {
			t.Fatalf("duplicate ordinal %d recorded across concurrent sends", entry.Ordinal)
		}
		seen[entry.Ordinal] = struct{}{}
		require.Equal(t, ReplyClientRequestID(identity, entry.Ordinal), entry.ClientRequestID,
			"recorded ordinal and clientRequestID must come from the same issuance")
	}
}
