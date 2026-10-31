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
	"reflect"
	"testing"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
)

func capturedClientRequestID(t *testing.T, opts []iris.SendOption) string {
	t.Helper()

	clientRequestID, _ := capturedSendOptions(t, opts)

	return clientRequestID
}

func capturedSendOptions(t *testing.T, opts []iris.SendOption) (clientRequestID, threadID string) {
	t.Helper()

	if len(opts) == 0 {
		return "", ""
	}

	// iris.SendOption이 적용하는 구조체가 비공개라 리플렉션 외에는 적용값을 읽을 수 없다.
	box := reflect.New(reflect.TypeFor[iris.SendOption]().In(0).Elem())

	for _, opt := range opts {
		reflect.ValueOf(opt).Call([]reflect.Value{box})
	}

	return sendOptionStringField(t, box, "ClientRequestID"), sendOptionStringField(t, box, "ThreadID")
}

func sendOptionStringField(t *testing.T, box reflect.Value, name string) string {
	t.Helper()

	field := box.Elem().FieldByName(name)
	require.True(t, field.IsValid(), "send option field %s is missing", name)

	value, ok := reflect.TypeAssert[*string](field)
	require.True(t, ok, "send option field %s is not *string", name)

	if value == nil {
		return ""
	}

	return *value
}

type stubBotClient struct {
	acceptErr   error
	sendErr     error
	richTextErr error
	accepted    *iris.ReplyAcceptedResponse

	imageErr      error
	imageAccepted *iris.ReplyAcceptedResponse
	onImage       func(call int)
	multiErr      error
	multiAccepted *iris.ReplyAcceptedResponse
	onMultiImage  func(call int)

	acceptCalls          int
	richTextCalls        int
	markdownCalls        int
	imageCalls           int
	multiImageCalls      int
	statusCalls          int
	lastRoom             string
	lastMessage          string
	lastOptsLen          int
	lastOpts             []iris.SendOption
	optsByAttempt        [][]iris.SendOption
	lastImage            []byte
	lastImages           [][]byte
	lastImageRoom        string
	imagesByAttempt      [][]byte
	multiImagesByAttempt [][][]byte
}

func (c *stubBotClient) recordSend(room, message string, opts []iris.SendOption) {
	c.lastRoom = room
	c.lastMessage = message
	c.lastOptsLen = len(opts)
	c.lastOpts = append([]iris.SendOption(nil), opts...)
	c.optsByAttempt = append(c.optsByAttempt, c.lastOpts)
}

func (c *stubBotClient) SendMessage(_ context.Context, room, message string, opts ...iris.SendOption) error {
	c.recordSend(room, message, opts)

	return c.sendErr
}

func (c *stubBotClient) SendMessageAccepted(_ context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.acceptCalls++
	c.recordSend(room, message, opts)

	if c.acceptErr != nil {
		return nil, c.acceptErr
	}

	if c.accepted != nil {
		return c.accepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-default"}, nil
}

func (c *stubBotClient) SendImage(_ context.Context, room string, imageData []byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.imageCalls++

	c.lastImageRoom = room
	c.lastImage = imageData
	c.imagesByAttempt = append(c.imagesByAttempt, imageData)
	c.lastOptsLen = len(opts)
	c.lastOpts = append([]iris.SendOption(nil), opts...)
	c.optsByAttempt = append(c.optsByAttempt, c.lastOpts)

	if c.onImage != nil {
		c.onImage(c.imageCalls)
	}

	if c.imageErr != nil {
		return nil, c.imageErr
	}

	if c.imageAccepted != nil {
		return c.imageAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-image"}, nil
}

func (c *stubBotClient) SendMultipleImages(_ context.Context, room string, images [][]byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.multiImageCalls++

	c.lastImageRoom = room
	c.lastImages = images
	c.multiImagesByAttempt = append(c.multiImagesByAttempt, images)
	c.lastOptsLen = len(opts)
	c.lastOpts = append([]iris.SendOption(nil), opts...)
	c.optsByAttempt = append(c.optsByAttempt, c.lastOpts)

	if c.onMultiImage != nil {
		c.onMultiImage(c.multiImageCalls)
	}

	if c.multiErr != nil {
		return nil, c.multiErr
	}

	if c.multiAccepted != nil {
		return c.multiAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-images"}, nil
}

func (c *stubBotClient) SendRichText(_ context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.richTextCalls++
	c.recordSend(room, markdown, opts)

	if c.richTextErr != nil {
		return nil, c.richTextErr
	}

	if c.accepted != nil {
		return c.accepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-markdown"}, nil
}

func (c *stubBotClient) SendMarkdown(_ context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.markdownCalls++
	c.recordSend(room, markdown, opts)

	return &iris.ReplyAcceptedResponse{RequestID: "r-markdown"}, nil
}

// GetReplyStatus는 직접 발송 경로가 handoff 상태를 조회하지 않는다는 것을 세기 위해서만 둔다.
func (c *stubBotClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++

	return &iris.ReplyStatusSnapshot{}, nil
}

func (c *stubBotClient) Ping(context.Context) error { return nil }

func TestWithThreadIDAndFromContext(t *testing.T) {
	t.Parallel()

	t.Run("empty and whitespace threadID leaves context untagged", func(t *testing.T) {
		t.Parallel()

		for _, in := range []string{"", "   ", "\t\n"} {
			ctx := WithThreadID(t.Context(), in)
			_, ok := ThreadIDFromContext(ctx)
			assert.False(t, ok, "input %q", in)
		}
	})

	t.Run("valid threadID round trips trimmed", func(t *testing.T) {
		t.Parallel()

		ctx := WithThreadID(t.Context(), "  t-1  ")
		id, ok := ThreadIDFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "t-1", id)
	})

	t.Run("missing value returns false", func(t *testing.T) {
		t.Parallel()

		id, ok := ThreadIDFromContext(t.Context())
		assert.False(t, ok)
		assert.Empty(t, id)
	})
}

func TestWithReplyIdentityAndFromContext(t *testing.T) {
	t.Parallel()

	t.Run("empty and whitespace identity leaves context untagged", func(t *testing.T) {
		t.Parallel()

		for _, in := range []string{"", "  ", "\t"} {
			ctx := WithReplyIdentity(t.Context(), in)
			_, ok := ReplyIdentityFromContext(ctx)
			assert.False(t, ok, "input %q", in)
		}
	})

	t.Run("valid identity round trips trimmed", func(t *testing.T) {
		t.Parallel()

		ctx := WithReplyIdentity(t.Context(), "  user-1 ")
		id, ok := ReplyIdentityFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "user-1", id)
	})
}

func TestThreadIDAndReplyIdentityKeysAreIndependent(t *testing.T) {
	t.Parallel()

	threadOnly := WithThreadID(t.Context(), "t-1")
	_, hasIdentity := ReplyIdentityFromContext(threadOnly)
	assert.False(t, hasIdentity)

	identityOnly := WithReplyIdentity(t.Context(), "u-1")
	_, hasThread := ThreadIDFromContext(identityOnly)
	assert.False(t, hasThread)

	both := WithReplyIdentity(WithThreadID(t.Context(), "t-2"), "u-2")
	tid, ok := ThreadIDFromContext(both)
	require.True(t, ok)
	assert.Equal(t, "t-2", tid)

	rid, ok := ReplyIdentityFromContext(both)
	require.True(t, ok)
	assert.Equal(t, "u-2", rid)
}

func TestAppendReplyClientRequestID(t *testing.T) {
	t.Parallel()

	base := []iris.SendOption{iris.WithThreadID("t-1")}
	got := appendReplyClientRequestID(base, "hololive:v1:message:m-1:reply:0")
	assert.Len(t, got, 2)
	assert.Len(t, base, 1, "original slice must not be mutated")

	fromNil := appendReplyClientRequestID(nil, "hololive:v1:message:m-1:reply:0")
	assert.Len(t, fromNil, 1)

	blank := appendReplyClientRequestID(base, "")
	assert.Len(t, blank, 1, "blank client request id must not be attached")
}

func TestCommandTransportSendError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("nil transport returns wrapped configuration error", func(t *testing.T) {
		t.Parallel()

		var tr *CommandTransport

		err := tr.SendError(ctx, "room", "some_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send error message")
	})

	// 코드 대체 문구가 없으므로 formatter나 문구가 없으면 빈 메시지를 보내지 않고 실패한다.
	t.Run("nil formatter fails without sending", func(t *testing.T) {
		t.Parallel()

		writer := &recordingReplyOutbox{}
		require.Error(t, tr(writer).SendError(inboundCtx(ctx), "room", "any_key"))
		assert.Empty(t, writer.snapshot())
	})

	t.Run("formatter without strings fails without sending", func(t *testing.T) {
		t.Parallel()

		writer := &recordingReplyOutbox{}
		formatter := messageformatter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(writer, formatter)
		require.Error(t, transport.SendError(inboundCtx(ctx), "room", "totally_unknown_key"))
		assert.Empty(t, writer.snapshot())
	})
}

func tr(writer ReplyOutboxWriter) *CommandTransport {
	return NewCommandTransport(writer, nil)
}

func inboundCtx(ctx context.Context) context.Context {
	return WithRoomChat(WithReplyIdentity(ctx, "message:m-1"), "OM", "")
}
