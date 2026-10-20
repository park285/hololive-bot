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
	"net/http"
	"reflect"
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const testReplyRequestID = "r-1"

type stubAcceptedSender struct {
	acceptErr     error
	accepted      *iris.ReplyAcceptedResponse
	acceptCalls   int
	onAccept      func(call int)
	optsByAttempt [][]iris.SendOption
}

func (s *stubAcceptedSender) SendMessageAccepted(_ context.Context, _, _ string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	s.acceptCalls++

	s.optsByAttempt = append(s.optsByAttempt, append([]iris.SendOption(nil), opts...))

	if s.onAccept != nil {
		s.onAccept(s.acceptCalls)
	}

	if s.acceptErr != nil {
		return nil, s.acceptErr
	}

	if s.accepted != nil {
		return s.accepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-accepted"}, nil
}

func (s *stubAcceptedSender) SendMarkdown(ctx context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	out, err := s.SendMessageAccepted(ctx, room, markdown, opts...)
	if err != nil {
		return nil, fmt.Errorf("send message accepted: %w", err)
	}

	return out, nil
}

func lostAdmissionResponseError() error {
	return fmt.Errorf("post /reply: %w", iris.ErrTransport)
}

func irisConflictWithCode(ctx context.Context, t *testing.T, code string) error {
	t.Helper()

	body := "{}"

	if code != "" {
		body = fmt.Sprintf(`{"code":%q}`, code)
	}

	return fmt.Errorf("iris conflict response: %w", testutil.IrisHTTPError(ctx, t, http.StatusConflict, body))
}

func replyLanesUnderTest() map[string]func(*stubAcceptedSender) replySendFunc {
	return map[string]func(*stubAcceptedSender) replySendFunc{
		"accepted": func(s *stubAcceptedSender) replySendFunc { return s.SendMessageAccepted },
		"markdown": func(s *stubAcceptedSender) replySendFunc { return s.SendMarkdown },
	}
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
	markdownErr error
	accepted    *iris.ReplyAcceptedResponse

	imageErr      error
	imageAccepted *iris.ReplyAcceptedResponse
	onImage       func(call int)
	multiErr      error
	multiAccepted *iris.ReplyAcceptedResponse
	onMultiImage  func(call int)

	pingResult bool

	acceptCalls          int
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

func (c *stubBotClient) SendMarkdown(_ context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.markdownCalls++
	c.recordSend(room, markdown, opts)

	if c.markdownErr != nil {
		return nil, c.markdownErr
	}

	if c.accepted != nil {
		return c.accepted, nil
	}

	return &iris.ReplyAcceptedResponse{RequestID: "r-markdown"}, nil
}

// GetReplyStatus는 직접 발송 경로가 handoff 상태를 조회하지 않는다는 것을 세기 위해서만 둔다.
func (c *stubBotClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++

	return &iris.ReplyStatusSnapshot{}, nil
}

func (c *stubBotClient) Ping(context.Context) bool { return c.pingResult }

func (c *stubBotClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}

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

func TestSendReply(t *testing.T) {
	t.Parallel()

	for laneName, newLane := range replyLanesUnderTest() {
		t.Run(laneName+" lane", func(t *testing.T) {
			t.Parallel()

			testSendReplyAdmission(t, newLane)
			testSendReplyHandoff(t, newLane)
		})
	}
}

func testSendReplyAdmission(t *testing.T, newLane func(*stubAcceptedSender) replySendFunc) {
	t.Helper()

	ctx := t.Context()

	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("non transport accept error is returned without re-post", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: errors.New("nope")}
		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.Equal(t, 1, s.acceptCalls, "rejected admission must not be re-posted")
	})

	t.Run("lost admission response is re-posted once then reported unknown", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}
		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.True(t, IsReplyOutcomeUnknown(err))
		assert.Equal(t, replyAdmissionMaxAttempts, s.acceptCalls)
	})

	t.Run("a rejection after a lost attempt stays unknown, not a plain failure", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}

		s.onAccept = func(call int) {
			if call >= 2 {
				s.acceptErr = errors.New("iris returned 500")
			}
		}

		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.True(t, IsReplyOutcomeUnknown(err),
			"the lost first attempt may already have been admitted, so the rejection is not authoritative")
		assert.Equal(t, replyAdmissionMaxAttempts, s.acceptCalls)
	})

	t.Run("lost admission response without a stable id is not re-posted", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}
		err := sendReply(ctx, newLane(s), "room", "msg", "", nil)
		require.Error(t, err)
		assert.True(t, IsReplyOutcomeUnknown(err))
		assert.Equal(t, 1, s.acceptCalls)
	})
}

func testSendReplyHandoff(t *testing.T, newLane func(*stubAcceptedSender) replySendFunc) {
	t.Helper()

	ctx := t.Context()

	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("admission with a request id ends the send without waiting for handoff", func(t *testing.T) {
		s := &stubAcceptedSender{accepted: &iris.ReplyAcceptedResponse{RequestID: testReplyRequestID}}
		require.NoError(t, sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil))
		assert.Equal(t, 1, s.acceptCalls)
	})

	t.Run("blank request id is outcome unknown without re-post", func(t *testing.T) {
		s := &stubAcceptedSender{accepted: &iris.ReplyAcceptedResponse{RequestID: "   "}}
		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.True(t, IsReplyOutcomeUnknown(err))
		assert.Equal(t, 1, s.acceptCalls)
	})

	t.Run("missing admission response is outcome unknown without re-post", func(t *testing.T) {
		var (
			calls int
			// 202 본문이 비어 온 경우를 재현한다.
			emptyBody *iris.ReplyAcceptedResponse
		)

		emptyAdmission := func(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
			calls++

			return emptyBody, nil
		}
		err := sendReply(ctx, emptyAdmission, "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.True(t, IsReplyOutcomeUnknown(err))
		assert.Equal(t, 1, calls)
	})
}

func TestSendReplyConflictReissue(t *testing.T) {
	t.Parallel()

	for laneName, newLane := range replyLanesUnderTest() {
		t.Run(laneName+" lane", func(t *testing.T) {
			t.Parallel()

			testSendReplyConflictReissue(t, newLane)
			testSendReplyConflictReissueTerminal(t, newLane)
		})
	}
}

func testSendReplyConflictReissue(t *testing.T, newLane func(*stubAcceptedSender) replySendFunc) {
	t.Helper()

	ctx := t.Context()

	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("failed conflict reissues once and succeeds", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		s.onAccept = func(call int) {
			if call == 2 {
				s.acceptErr = nil
			}
		}

		require.NoError(t, sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil))
		require.Len(t, s.optsByAttempt, 2)

		first, _ := capturedSendOptions(t, s.optsByAttempt[0])
		second, _ := capturedSendOptions(t, s.optsByAttempt[1])
		assert.Equal(t, clientRequestID, first)
		assert.Equal(t, clientRequestID+":r1", second)
	})

	t.Run("failed conflict exhausts two reissue generations", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDFailed, iris.HTTPErrorCode(err))
		require.Len(t, s.optsByAttempt, iris.ReplyReissueMaxGenerations+1)

		wantIDs := []string{clientRequestID, clientRequestID + ":r1", clientRequestID + ":r2"}

		for i, opts := range s.optsByAttempt {
			got, _ := capturedSendOptions(t, opts)
			assert.Equal(t, wantIDs[i], got)
		}
	})

	t.Run("already exists on a reissued generation is terminal", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		s.onAccept = func(call int) {
			if call == 2 {
				s.acceptErr = irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDAlreadyExists)
			}
		}

		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDAlreadyExists, iris.HTTPErrorCode(err))
		require.Len(t, s.optsByAttempt, 2)

		last, _ := capturedSendOptions(t, s.optsByAttempt[1])
		assert.Equal(t, clientRequestID+":r1", last)
	})
}

func testSendReplyConflictReissueTerminal(t *testing.T, newLane func(*stubAcceptedSender) replySendFunc) {
	t.Helper()

	ctx := t.Context()

	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("other conflicts remain terminal without reissue", func(t *testing.T) {
		for _, code := range []string{
			iris.HTTPErrorCodeClientRequestIDOutcomeUnknown,
			iris.HTTPErrorCodeClientRequestIDPayloadMismatch,
			iris.HTTPErrorCodeClientRequestIDAlreadyExists,
			"IRIS_FUTURE_CONFLICT",
			"",
		} {
			t.Run(code, func(t *testing.T) {
				s := &stubAcceptedSender{acceptErr: irisConflictWithCode(ctx, t, code)}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.Equal(t, code, iris.HTTPErrorCode(err))
				assert.Equal(t, 1, s.acceptCalls)
			})
		}
	})

	t.Run("failed conflict after a lost attempt advances generation", func(t *testing.T) {
		s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}

		s.onAccept = func(call int) {
			switch call {
			case 2:
				s.acceptErr = irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)
			case 3:
				s.acceptErr = nil
			}
		}

		require.NoError(t, sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil))
		require.Len(t, s.optsByAttempt, 3)

		first, _ := capturedSendOptions(t, s.optsByAttempt[0])
		second, _ := capturedSendOptions(t, s.optsByAttempt[1])
		third, _ := capturedSendOptions(t, s.optsByAttempt[2])
		assert.Equal(t, clientRequestID, first)
		assert.Equal(t, first, second)
		assert.Equal(t, clientRequestID+":r1", third)
	})
}

func TestCommandTransportSendMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("nil transport returns configuration error", func(t *testing.T) {
		t.Parallel()

		var tr *CommandTransport

		err := tr.SendMessage(ctx, "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		t.Parallel()

		tr := NewCommandTransport(nil, nil)
		err := tr.SendMessage(ctx, "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("plain accept error returns after single attempt", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{acceptErr: errors.New("iris down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(inboundCtx(ctx), "room-x", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "iris down")
		assert.Equal(t, 1, c.acceptCalls)
	})

	t.Run("admission ends the send without a reply status query", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, 0, c.statusCalls)
		assert.Equal(t, "hi", c.lastMessage)
	})

	t.Run("admission without a request id surfaces outcome unknown", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{accepted: &iris.ReplyAcceptedResponse{}}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(inboundCtx(ctx), "room", "hi")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		assert.Equal(t, 1, c.acceptCalls, "unknown outcome must not trigger a second POST")
	})
}

func TestCommandTransportSendMessageOptions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("thread id in context adds an extra send option", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(WithThreadID(inboundCtx(ctx), "t-1"), "room", "hi"))
		assert.Equal(t, 2, c.lastOptsLen)
	})

	t.Run("no thread id yields a single client-request-id option", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.lastOptsLen)
	})

	t.Run("without an inbound identity no client request id is attached", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(ctx, "room", "hi"))
		assert.Equal(t, 0, c.lastOptsLen)

		requestID, _ := capturedSendOptions(t, c.lastOpts)
		assert.Empty(t, requestID)
	})
}

func TestCommandTransportSendMessageMarkdownLane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	const wantRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("markdown lane sends through SendMarkdown with the stable request id", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room-md", "**hi**"))
		assert.Equal(t, 1, c.markdownCalls)
		assert.Equal(t, 0, c.acceptCalls)
		assert.Equal(t, "room-md", c.lastRoom)
		assert.Equal(t, "**hi**", c.lastMessage)
		assert.Equal(t, 1, c.lastOptsLen)

		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, wantRequestID, requestID)
		assert.Empty(t, threadID)
		assert.Equal(t, 0, c.statusCalls)
	})

	t.Run("regular chat renders unicode on the accepted lane", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		chatCtx := WithRoomChat(inboundCtx(ctx), "MultiChat", "")
		require.NoError(t, tr.SendMessage(chatCtx, "room-reg", "## **hi**"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, "【𝗵𝗶】", c.lastMessage)
	})

	t.Run("disabled flag keeps the accepted lane", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(false))

		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "**hi**"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, "𝗵𝗶", c.lastMessage)

		requestID, _ := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, wantRequestID, requestID)
	})
}

func TestCommandTransportSendMessageMarkdownLaneAdmission(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	const wantRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("send error returns after a single markdown attempt", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{markdownErr: errors.New("iris down")}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		err := tr.SendMessage(inboundCtx(ctx), "room-x", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "iris down")
		assert.Equal(t, 1, c.markdownCalls)
	})

	t.Run("lost admission response is re-posted with the same request id", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{markdownErr: lostAdmissionResponseError()}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		err := tr.SendMessage(inboundCtx(ctx), "room", "hi")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		assert.Equal(t, replyAdmissionMaxAttempts, c.markdownCalls)
		require.Len(t, c.optsByAttempt, replyAdmissionMaxAttempts)

		first, _ := capturedSendOptions(t, c.optsByAttempt[0])
		second, _ := capturedSendOptions(t, c.optsByAttempt[1])
		assert.Equal(t, wantRequestID, first)
		assert.Equal(t, first, second, "re-post must reuse the same clientRequestId")
	})

	t.Run("thread id is forwarded on the markdown lane", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		require.NoError(t, tr.SendMessage(WithThreadID(inboundCtx(ctx), "t-1"), "room", "hi"))
		assert.Equal(t, 2, c.lastOptsLen)

		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Equal(t, wantRequestID, requestID)
	})

	t.Run("nil option is ignored", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, nil, WithMarkdownReplies(true))

		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.markdownCalls)
	})
}

func TestCommandTransportSendImage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("nil transport returns configuration error", func(t *testing.T) {
		t.Parallel()

		var tr *CommandTransport

		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		t.Parallel()

		tr := NewCommandTransport(nil, nil)
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("client error is wrapped without room id", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageErr: errors.New("img down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image: ")
		assert.Contains(t, err.Error(), "img down")
	})

	t.Run("success forwards bytes and returns nil", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		data := []byte{0x89, 0x50, 0x4E, 0x47}
		require.NoError(t, tr(c).SendImage(ctx, "room-1", data))
		assert.Equal(t, "room-1", c.lastImageRoom)
		assert.Equal(t, data, c.lastImage)
	})

	t.Run("admission without a request id is outcome unknown", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageAccepted: &iris.ReplyAcceptedResponse{}}
		err := tr(c).SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image: ")
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		assert.Equal(t, 1, c.imageCalls)
		assert.Equal(t, 0, c.statusCalls)
	})

	t.Run("lost admission response does not advance the image generation", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageErr: lostAdmissionResponseError()}
		err := tr(c).SendImage(inboundCtx(ctx), "room", []byte("x"))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		require.Len(t, c.optsByAttempt, 1)

		requestID, _ := capturedSendOptions(t, c.optsByAttempt[0])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)
	})
}

func TestCommandTransportSendImageConflictReissue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("failed conflict reissues with the same image payload", func(t *testing.T) {
		t.Parallel()

		data := []byte("image-payload")
		c := &stubBotClient{imageErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		c.onImage = func(call int) {
			if call == 2 {
				c.imageErr = nil
			}
		}

		require.NoError(t, tr(c).SendImage(inboundCtx(ctx), "room", data))
		require.Len(t, c.optsByAttempt, 2)
		require.Len(t, c.imagesByAttempt, 2)

		first, _ := capturedSendOptions(t, c.optsByAttempt[0])
		second, _ := capturedSendOptions(t, c.optsByAttempt[1])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", first)
		assert.Equal(t, first+":r1", second)
		assert.Equal(t, data, c.imagesByAttempt[0])
		assert.Equal(t, data, c.imagesByAttempt[1])
	})

	t.Run("failed conflict exhausts two image reissue generations", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		err := tr(c).SendImage(inboundCtx(ctx), "room", []byte("image-payload"))
		require.Error(t, err)
		require.ErrorIs(t, err, iris.ErrPermanent)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDFailed, iris.HTTPErrorCode(err))
		require.Len(t, c.optsByAttempt, iris.ReplyReissueMaxGenerations+1)

		wantIDs := []string{
			"hololive:v1:message:m-1:reply:0",
			"hololive:v1:message:m-1:reply:0:r1",
			"hololive:v1:message:m-1:reply:0:r2",
		}

		for i, opts := range c.optsByAttempt {
			got, _ := capturedSendOptions(t, opts)
			assert.Equal(t, wantIDs[i], got)
		}
	})

	t.Run("other image conflicts remain terminal without reissue", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown)}

		err := tr(c).SendImage(inboundCtx(ctx), "room", []byte("image-payload"))
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown, iris.HTTPErrorCode(err))
		assert.Equal(t, 1, c.imageCalls)
	})

	t.Run("a plain rejection stays a terminal failure", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{imageErr: errors.New("img rejected")}
		err := tr(c).SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrReplyOutcomeUnknown)
	})
}

func TestCommandTransportSendImages(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("multiple images are sent as one album", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		images := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

		require.NoError(t, tr(c).SendImages(ctx, "room-1", images))
		assert.Equal(t, "room-1", c.lastImageRoom)
		assert.Equal(t, images, c.lastImages)
		assert.Nil(t, c.lastImage)
	})

	t.Run("single image preserves the single image lane", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}

		require.NoError(t, tr(c).SendImages(ctx, "room-1", [][]byte{[]byte("one")}))
		assert.Equal(t, []byte("one"), c.lastImage)
		assert.Nil(t, c.lastImages)
	})

	t.Run("thread and stable request id are forwarded, one ordinal per emission", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		images := [][]byte{[]byte("one"), []byte("two")}
		threadCtx := WithReplyIdentity(WithThreadID(ctx, "t-1"), "message:m-1")

		require.NoError(t, tr(c).SendImages(threadCtx, "room-1", images))
		assert.Equal(t, 2, c.lastOptsLen)

		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)

		require.NoError(t, tr(c).SendImages(threadCtx, "room-1", images))

		requestID, _ = capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "hololive:v1:message:m-1:reply:1", requestID,
			"a second emission inside one inbound message must not reuse the first ordinal")
	})

	t.Run("without an inbound identity no client request id is attached", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}

		require.NoError(t, tr(c).SendImages(WithThreadID(ctx, "t-1"), "room-1", [][]byte{[]byte("one"), []byte("two")}))

		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Empty(t, requestID)
	})
}

func TestCommandTransportSendImagesFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("client error is wrapped without room id", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{multiErr: errors.New("album down")}

		err := tr(c).SendImages(ctx, "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send images: ")
		assert.Contains(t, err.Error(), "album down")
		assert.NotErrorIs(t, err, ErrReplyOutcomeUnknown)
	})

	t.Run("lost admission response does not advance the album generation", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{multiErr: lostAdmissionResponseError()}

		err := tr(c).SendImages(inboundCtx(ctx), "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		require.Len(t, c.optsByAttempt, 1)

		requestID, _ := capturedSendOptions(t, c.optsByAttempt[0])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)
	})

	t.Run("failed conflict reissues with the same album payload", func(t *testing.T) {
		t.Parallel()

		images := [][]byte{[]byte("one"), []byte("two")}
		c := &stubBotClient{multiErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		c.onMultiImage = func(call int) {
			if call == 2 {
				c.multiErr = nil
			}
		}

		require.NoError(t, tr(c).SendImages(inboundCtx(ctx), "room", images))
		require.Len(t, c.optsByAttempt, 2)
		require.Len(t, c.multiImagesByAttempt, 2)

		first, _ := capturedSendOptions(t, c.optsByAttempt[0])
		second, _ := capturedSendOptions(t, c.optsByAttempt[1])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", first)
		assert.Equal(t, first+":r1", second)
		assert.Equal(t, images, c.multiImagesByAttempt[0])
		assert.Equal(t, images, c.multiImagesByAttempt[1])
	})
}

func TestCommandTransportSendImagesReissueExhaustion(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("failed conflict exhausts two album reissue generations", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{multiErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDFailed)}

		err := tr(c).SendImages(inboundCtx(ctx), "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		require.ErrorIs(t, err, iris.ErrPermanent)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDFailed, iris.HTTPErrorCode(err))
		require.Len(t, c.optsByAttempt, iris.ReplyReissueMaxGenerations+1)

		wantIDs := []string{
			"hololive:v1:message:m-1:reply:0",
			"hololive:v1:message:m-1:reply:0:r1",
			"hololive:v1:message:m-1:reply:0:r2",
		}

		for i, opts := range c.optsByAttempt {
			got, _ := capturedSendOptions(t, opts)
			assert.Equal(t, wantIDs[i], got)
		}
	})

	t.Run("other album conflicts remain terminal without reissue", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{multiErr: irisConflictWithCode(ctx, t, iris.HTTPErrorCodeClientRequestIDPayloadMismatch)}

		err := tr(c).SendImages(inboundCtx(ctx), "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDPayloadMismatch, iris.HTTPErrorCode(err))
		assert.Equal(t, 1, c.multiImageCalls)
	})

	t.Run("admission without a request id is outcome unknown", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{multiAccepted: &iris.ReplyAcceptedResponse{}}

		err := tr(c).SendImages(ctx, "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send images: ")
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		assert.Equal(t, 1, c.multiImageCalls)
		assert.Equal(t, 0, c.statusCalls)
	})
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

		c := &stubBotClient{}
		require.Error(t, tr(c).SendError(ctx, "room", "any_key"))
		assert.Empty(t, c.lastMessage)
	})

	t.Run("formatter without strings fails without sending", func(t *testing.T) {
		t.Parallel()

		c := &stubBotClient{}
		formatter := messageformatter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(c, formatter)
		require.Error(t, transport.SendError(ctx, "room", "totally_unknown_key"))
		assert.Empty(t, c.lastMessage)
	})
}

func tr(c iris.BotClient) *CommandTransport {
	return NewCommandTransport(c, nil)
}

func inboundCtx(ctx context.Context) context.Context {
	return WithRoomChat(WithReplyIdentity(ctx, "message:m-1"), "OM", "")
}
