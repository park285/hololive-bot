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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type statusResult struct {
	snap *iris.ReplyStatusSnapshot
	err  error
}

type stubStatusGetter struct {
	snap   *iris.ReplyStatusSnapshot
	err    error
	calls  int
	onCall func(call int)
}

func (s *stubStatusGetter) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	s.calls++
	if s.onCall != nil {
		s.onCall(s.calls)
	}
	return s.snap, s.err
}

type stubAcceptedSender struct {
	stubStatusGetter
	acceptErr   error
	accepted    *iris.ReplyAcceptedResponse
	acceptCalls int
}

func (s *stubAcceptedSender) SendMessageAccepted(_ context.Context, _, _ string, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	s.acceptCalls++
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	if s.accepted != nil {
		return s.accepted, nil
	}
	return &iris.ReplyAcceptedResponse{RequestID: "r-accepted"}, nil
}

type stubBotClient struct {
	acceptErr error
	sendErr   error
	accepted  *iris.ReplyAcceptedResponse

	imageErr      error
	imageAccepted *iris.ReplyAcceptedResponse
	multiErr      error
	multiAccepted *iris.ReplyAcceptedResponse

	statuses   []statusResult
	pingResult bool

	acceptCalls   int
	statusCalls   int
	lastRoom      string
	lastMessage   string
	lastOptsLen   int
	lastImageRoom string
	lastImage     []byte
	lastImages    [][]byte
}

func (c *stubBotClient) SendMessage(_ context.Context, room, message string, opts ...iris.SendOption) error {
	c.lastRoom = room
	c.lastMessage = message
	c.lastOptsLen = len(opts)
	return c.sendErr
}

func (c *stubBotClient) SendMessageAccepted(_ context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.acceptCalls++
	c.lastRoom = room
	c.lastMessage = message
	c.lastOptsLen = len(opts)
	if c.acceptErr != nil {
		return nil, c.acceptErr
	}
	if c.accepted != nil {
		return c.accepted, nil
	}
	return &iris.ReplyAcceptedResponse{RequestID: "r-default"}, nil
}

func (c *stubBotClient) SendImage(_ context.Context, room string, imageData []byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.lastImageRoom = room
	c.lastImage = imageData
	if c.imageErr != nil {
		return nil, c.imageErr
	}
	return c.imageAccepted, nil
}

func (c *stubBotClient) SendMultipleImages(_ context.Context, room string, images [][]byte, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.lastImageRoom = room
	c.lastImages = images
	if c.multiErr != nil {
		return nil, c.multiErr
	}
	return c.multiAccepted, nil
}

func (c *stubBotClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}

func (c *stubBotClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++
	if len(c.statuses) == 0 {
		return &iris.ReplyStatusSnapshot{State: "handoff_completed"}, nil
	}
	r := c.statuses[0]
	c.statuses = c.statuses[1:]
	return r.snap, r.err
}

func (c *stubBotClient) Ping(context.Context) bool { return c.pingResult }

func (c *stubBotClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}

func TestWithThreadIDAndFromContext(t *testing.T) {
	t.Parallel()

	t.Run("empty and whitespace threadID leaves context untagged", func(t *testing.T) {
		for _, in := range []string{"", "   ", "\t\n"} {
			ctx := WithThreadID(context.Background(), in)
			_, ok := ThreadIDFromContext(ctx)
			assert.False(t, ok, "input %q", in)
		}
	})

	t.Run("valid threadID round trips trimmed", func(t *testing.T) {
		ctx := WithThreadID(context.Background(), "  t-1  ")
		id, ok := ThreadIDFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "t-1", id)
	})

	t.Run("nil context returns false", func(t *testing.T) {
		var nilCtx context.Context
		id, ok := ThreadIDFromContext(nilCtx)
		assert.False(t, ok)
		assert.Empty(t, id)
	})

	t.Run("missing value returns false", func(t *testing.T) {
		id, ok := ThreadIDFromContext(context.Background())
		assert.False(t, ok)
		assert.Empty(t, id)
	})
}

func TestWithReplyIdentityAndFromContext(t *testing.T) {
	t.Parallel()

	t.Run("empty and whitespace identity leaves context untagged", func(t *testing.T) {
		for _, in := range []string{"", "  ", "\t"} {
			ctx := WithReplyIdentity(context.Background(), in)
			_, ok := ReplyIdentityFromContext(ctx)
			assert.False(t, ok, "input %q", in)
		}
	})

	t.Run("valid identity round trips trimmed", func(t *testing.T) {
		ctx := WithReplyIdentity(context.Background(), "  user-1 ")
		id, ok := ReplyIdentityFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "user-1", id)
	})

	t.Run("nil context returns false", func(t *testing.T) {
		var nilCtx context.Context
		id, ok := ReplyIdentityFromContext(nilCtx)
		assert.False(t, ok)
		assert.Empty(t, id)
	})
}

func TestThreadIDAndReplyIdentityKeysAreIndependent(t *testing.T) {
	t.Parallel()

	threadOnly := WithThreadID(context.Background(), "t-1")
	_, hasIdentity := ReplyIdentityFromContext(threadOnly)
	assert.False(t, hasIdentity)

	identityOnly := WithReplyIdentity(context.Background(), "u-1")
	_, hasThread := ThreadIDFromContext(identityOnly)
	assert.False(t, hasThread)

	both := WithReplyIdentity(WithThreadID(context.Background(), "t-2"), "u-2")
	tid, ok := ThreadIDFromContext(both)
	require.True(t, ok)
	assert.Equal(t, "t-2", tid)
	rid, ok := ReplyIdentityFromContext(both)
	require.True(t, ok)
	assert.Equal(t, "u-2", rid)
}

func TestCommandReplyClientRequestIDBase(t *testing.T) {
	t.Parallel()

	const prefix = "hololive-bot:reply:"

	t.Run("deterministic golden for stable identity", func(t *testing.T) {
		got := commandReplyClientRequestIDBase("room-1", "hello", "user-1")
		assert.Equal(t, "hololive-bot:reply:034dcfe316104c905bb26cf85a04ab99", got)
	})

	t.Run("room whitespace is trimmed", func(t *testing.T) {
		assert.Equal(t,
			commandReplyClientRequestIDBase("room-1", "hello", "user-1"),
			commandReplyClientRequestIDBase("  room-1  ", "hello", "user-1"),
		)
	})

	t.Run("message whitespace is significant", func(t *testing.T) {
		assert.NotEqual(t,
			commandReplyClientRequestIDBase("room-1", "hello", "user-1"),
			commandReplyClientRequestIDBase("room-1", " hello ", "user-1"),
		)
		assert.Equal(t,
			"hololive-bot:reply:eadb2c124d9a532b59295c674c18b21b",
			commandReplyClientRequestIDBase("room-1", " hello ", "user-1"),
		)
	})

	t.Run("empty identity yields nondeterministic ids with stable shape", func(t *testing.T) {
		a := commandReplyClientRequestIDBase("room-1", "hello", "")
		b := commandReplyClientRequestIDBase("room-1", "hello", "")
		assert.NotEqual(t, a, b)
		for _, v := range []string{a, b} {
			assert.True(t, strings.HasPrefix(v, prefix))
			assert.Len(t, v, len(prefix)+32)
		}
	})

	t.Run("whitespace-only identity is treated as empty", func(t *testing.T) {
		a := commandReplyClientRequestIDBase("room-1", "hello", "   ")
		b := commandReplyClientRequestIDBase("room-1", "hello", "   ")
		assert.NotEqual(t, a, b)
	})
}

func TestCommandReplyIdentity(t *testing.T) {
	t.Parallel()

	assert.Empty(t, commandReplyIdentity(context.Background()))
	ctx := WithReplyIdentity(context.Background(), "user-9")
	assert.Equal(t, "user-9", commandReplyIdentity(ctx))
}

func TestAppendReplyClientRequestID(t *testing.T) {
	t.Parallel()

	base := []iris.SendOption{iris.WithThreadID("t-1")}
	got := appendReplyClientRequestID(base, "hololive-bot:reply:abcd", 2)
	assert.Len(t, got, 2)
	assert.Len(t, base, 1, "original slice must not be mutated")

	fromNil := appendReplyClientRequestID(nil, "hololive-bot:reply:abcd", 1)
	assert.Len(t, fromNil, 1)
}

func TestReplyStatusFailedError(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "iris reply r-1 failed", replyStatusFailedError{requestID: "r-1"}.Error())
	assert.Equal(t, "iris reply r-1 failed: boom", replyStatusFailedError{requestID: "r-1", detail: "boom"}.Error())
	assert.Equal(t, "iris reply r-1 failed", replyStatusFailedError{requestID: "r-1", detail: "   "}.Error())
}

func TestIsReplyStatusFailed(t *testing.T) {
	t.Parallel()

	assert.False(t, isReplyStatusFailed(nil))
	assert.False(t, isReplyStatusFailed(errors.New("other")))
	assert.True(t, isReplyStatusFailed(replyStatusFailedError{requestID: "r"}))
	assert.True(t, isReplyStatusFailed(fmt.Errorf("wrap: %w", replyStatusFailedError{requestID: "r"})))
}

func TestReplyStatusDetail(t *testing.T) {
	t.Parallel()

	assert.Empty(t, replyStatusDetail(&iris.ReplyStatusSnapshot{}))
	detail := "d1"
	assert.Equal(t, "d1", replyStatusDetail(&iris.ReplyStatusSnapshot{Detail: &detail}))
}

func TestReplyHandoffStatusResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state      string
		wantDone   bool
		wantFailed bool
	}{
		{"handoff_completed", true, false},
		{"delivered", true, false},
		{"sent", true, false},
		{"  HANDOFF_COMPLETED  ", true, false},
		{"Delivered", true, false},
		{"failed", true, true},
		{"  FAILED ", true, true},
		{"pending", false, false},
		{"", false, false},
		{"unknown", false, false},
	}

	for _, tc := range cases {
		done, err := replyHandoffStatusResult("r-1", &iris.ReplyStatusSnapshot{State: tc.state})
		assert.Equal(t, tc.wantDone, done, "state %q", tc.state)
		if tc.wantFailed {
			require.Error(t, err, "state %q", tc.state)
			assert.True(t, isReplyStatusFailed(err))
		} else {
			require.NoError(t, err, "state %q", tc.state)
		}
	}

	detail := "callback failed"
	_, err := replyHandoffStatusResult("r-2", &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iris reply r-2 failed: callback failed")
}

func TestCheckReplyHandoffStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("getter error is reported as done without error", func(t *testing.T) {
		done, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{err: errors.New("boom")}, "r")
		assert.True(t, done)
		require.NoError(t, err)
	})

	t.Run("nil status is not done", func(t *testing.T) {
		done, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{snap: nil}, "r")
		assert.False(t, done)
		require.NoError(t, err)
	})

	t.Run("completed status is done", func(t *testing.T) {
		done, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "sent"}}, "r")
		assert.True(t, done)
		require.NoError(t, err)
	})

	t.Run("failed status returns failed error", func(t *testing.T) {
		done, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}, "r")
		assert.True(t, done)
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
	})
}

func TestWaitReplyStatusPoll(t *testing.T) {
	t.Parallel()

	t.Run("canceled context returns true", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.True(t, waitReplyStatusPoll(ctx, make(chan time.Time)))
	})

	t.Run("tick returns false", func(t *testing.T) {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		assert.False(t, waitReplyStatusPoll(context.Background(), ch))
	})
}

func TestWaitForReplyHandoff(t *testing.T) {
	t.Parallel()

	t.Run("completes on first success", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}
		require.NoError(t, waitForReplyHandoff(context.Background(), g, "r"))
		assert.Equal(t, 1, g.calls)
	})

	t.Run("failed status returns failed error", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}
		err := waitForReplyHandoff(context.Background(), g, "r")
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
	})

	t.Run("getter error stops the loop and returns nil", func(t *testing.T) {
		g := &stubStatusGetter{err: errors.New("down")}
		require.NoError(t, waitForReplyHandoff(context.Background(), g, "r"))
		assert.Equal(t, 1, g.calls)
	})

	t.Run("context cancellation while pending returns nil", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		g := &stubStatusGetter{
			snap:   &iris.ReplyStatusSnapshot{State: "pending"},
			onCall: func(int) { cancel() },
		}
		require.NoError(t, waitForReplyHandoff(ctx, g, "r"))
	})
}

func TestWaitForAcceptedReplyHandoff(t *testing.T) {
	t.Parallel()

	t.Run("nil accepted skips polling", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}
		require.NoError(t, waitForAcceptedReplyHandoff(context.Background(), g, nil))
		assert.Equal(t, 0, g.calls)
	})

	t.Run("blank request id skips polling", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}
		require.NoError(t, waitForAcceptedReplyHandoff(context.Background(), g, &iris.ReplyAcceptedResponse{RequestID: "   "}))
		assert.Equal(t, 0, g.calls)
	})

	t.Run("request id triggers polling", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}
		err := waitForAcceptedReplyHandoff(context.Background(), g, &iris.ReplyAcceptedResponse{RequestID: "r-1"})
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
		assert.Equal(t, 1, g.calls)
	})
}

func TestSendAcceptedMessageAttempt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("accept error returns not done with error", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: errors.New("nope")}
		done, err := sendAcceptedMessageAttempt(ctx, s, "room", "msg")
		assert.False(t, done)
		require.Error(t, err)
		assert.Equal(t, 0, s.calls)
	})

	t.Run("accepted and completed returns done without error", func(t *testing.T) {
		s := &stubAcceptedSender{
			accepted:         &iris.ReplyAcceptedResponse{RequestID: "r-1"},
			stubStatusGetter: stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "sent"}},
		}
		done, err := sendAcceptedMessageAttempt(ctx, s, "room", "msg")
		assert.True(t, done)
		require.NoError(t, err)
	})

	t.Run("accepted then failed returns not done with failed error", func(t *testing.T) {
		detail := "boom"
		s := &stubAcceptedSender{
			accepted:         &iris.ReplyAcceptedResponse{RequestID: "r-1"},
			stubStatusGetter: stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
		}
		done, err := sendAcceptedMessageAttempt(ctx, s, "room", "msg")
		assert.False(t, done)
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
	})

	t.Run("blank request id skips polling and returns done", func(t *testing.T) {
		s := &stubAcceptedSender{accepted: &iris.ReplyAcceptedResponse{RequestID: ""}}
		done, err := sendAcceptedMessageAttempt(ctx, s, "room", "msg")
		assert.True(t, done)
		require.NoError(t, err)
		assert.Equal(t, 0, s.calls)
	})
}

func TestCommandTransportSendMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil transport returns configuration error", func(t *testing.T) {
		var tr *CommandTransport
		err := tr.SendMessage(ctx, "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		tr := NewCommandTransport(nil, nil)
		err := tr.SendMessage(ctx, "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("plain accept error returns after single attempt", func(t *testing.T) {
		c := &stubBotClient{acceptErr: errors.New("iris down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(ctx, "room-x", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message to room room-x")
		assert.Contains(t, err.Error(), "iris down")
		assert.Equal(t, 1, c.acceptCalls)
	})

	t.Run("success on first attempt", func(t *testing.T) {
		c := &stubBotClient{statuses: []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}}}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(ctx, "room", "hi"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, "hi", c.lastMessage)
	})

	t.Run("retries once after failed status then succeeds", func(t *testing.T) {
		detail := "cb failed"
		c := &stubBotClient{statuses: []statusResult{
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
			{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}},
		}}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(ctx, "room", "hi"))
		assert.Equal(t, 2, c.acceptCalls)
	})

	t.Run("failed status on both attempts returns wrapped failed error", func(t *testing.T) {
		detail := "cb failed"
		c := &stubBotClient{statuses: []statusResult{
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
		}}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(ctx, "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message to room room")
		assert.Contains(t, err.Error(), "cb failed")
		assert.Equal(t, 2, c.acceptCalls)
	})

	t.Run("thread id in context adds an extra send option", func(t *testing.T) {
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(WithThreadID(ctx, "t-1"), "room", "hi"))
		assert.Equal(t, 2, c.lastOptsLen)
	})

	t.Run("no thread id yields a single client-request-id option", func(t *testing.T) {
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(ctx, "room", "hi"))
		assert.Equal(t, 1, c.lastOptsLen)
	})
}

func TestCommandTransportSendImage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil transport returns configuration error", func(t *testing.T) {
		var tr *CommandTransport
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		tr := NewCommandTransport(nil, nil)
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("client error is wrapped with room", func(t *testing.T) {
		c := &stubBotClient{imageErr: errors.New("img down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image to room room")
		assert.Contains(t, err.Error(), "img down")
	})

	t.Run("success forwards bytes and returns nil", func(t *testing.T) {
		c := &stubBotClient{}
		data := []byte{0x89, 0x50, 0x4E, 0x47}
		require.NoError(t, tr(c).SendImage(ctx, "room-1", data))
		assert.Equal(t, "room-1", c.lastImageRoom)
		assert.Equal(t, data, c.lastImage)
	})

	t.Run("failed reply status is wrapped with detail", func(t *testing.T) {
		detail := "image lease last modified mismatch"
		c := &stubBotClient{
			imageAccepted: &iris.ReplyAcceptedResponse{RequestID: "r-img"},
			statuses:      []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}}},
		}
		err := tr(c).SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image to room room")
		assert.Contains(t, err.Error(), "image lease last modified mismatch")
	})
}

func TestCommandTransportSendMultipleImages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil transport returns configuration error", func(t *testing.T) {
		var tr *CommandTransport
		err := tr.SendMultipleImages(ctx, "room", [][]byte{[]byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		tr := NewCommandTransport(nil, nil)
		err := tr.SendMultipleImages(ctx, "room", [][]byte{[]byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("empty batch is rejected", func(t *testing.T) {
		err := tr(&stubBotClient{}).SendMultipleImages(ctx, "room", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "images must not be empty")
	})

	t.Run("client error is wrapped with room", func(t *testing.T) {
		c := &stubBotClient{multiErr: errors.New("multi down")}
		err := tr(c).SendMultipleImages(ctx, "room", [][]byte{[]byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send multiple images to room room")
		assert.Contains(t, err.Error(), "multi down")
	})

	t.Run("success forwards image slices", func(t *testing.T) {
		c := &stubBotClient{}
		images := [][]byte{{1, 2}, {3, 4}}
		require.NoError(t, tr(c).SendMultipleImages(ctx, "room", images))
		require.Len(t, c.lastImages, 2)
		assert.Equal(t, images[0], c.lastImages[0])
		assert.Equal(t, images[1], c.lastImages[1])
	})

	t.Run("failed reply status is wrapped with detail", func(t *testing.T) {
		detail := "image lease last modified mismatch"
		c := &stubBotClient{
			multiAccepted: &iris.ReplyAcceptedResponse{RequestID: "r-multi"},
			statuses:      []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}}},
		}
		err := tr(c).SendMultipleImages(ctx, "room", [][]byte{[]byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send multiple images to room room")
		assert.Contains(t, err.Error(), "image lease last modified mismatch")
	})
}

func TestCommandTransportSendError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil transport returns wrapped configuration error", func(t *testing.T) {
		var tr *CommandTransport
		err := tr.SendError(ctx, "room", "some_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send error message")
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil formatter sends fallback sentinel", func(t *testing.T) {
		c := &stubBotClient{}
		require.NoError(t, tr(c).SendError(ctx, "room", "any_key"))
		assert.Equal(t, "room", c.lastRoom)
		assert.Equal(t, messagestrings.FallbackSentinel, c.lastMessage)
	})

	t.Run("formatter without strings resolves unknown key to sentinel", func(t *testing.T) {
		c := &stubBotClient{}
		formatter := adapter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(c, formatter)
		require.NoError(t, transport.SendError(ctx, "room", "totally_unknown_key"))
		assert.Equal(t, messagestrings.FallbackSentinel, c.lastMessage)
	})
}

func tr(c iris.BotClient) *CommandTransport {
	return NewCommandTransport(c, nil)
}
