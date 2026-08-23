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
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
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
	return s.SendMessageAccepted(ctx, room, markdown, opts...)
}

func lostAdmissionResponseError() error {
	return fmt.Errorf("post /reply: %w", iris.ErrTransport)
}

func irisConflictWithCode(code string) error {
	body := "{}"
	if code != "" {
		body = fmt.Sprintf(`{"code":%q}`, code)
	}
	return &iris.HTTPError{
		StatusCode: http.StatusConflict,
		URL:        "https://iris/reply",
		Body:       body,
	}
}

func replyLanesUnderTest() map[string]func(*stubAcceptedSender) replyLane {
	return map[string]func(*stubAcceptedSender) replyLane{
		"accepted": func(s *stubAcceptedSender) replyLane {
			return replyLane{send: s.SendMessageAccepted, getter: s}
		},
		"markdown": func(s *stubAcceptedSender) replyLane {
			return replyLane{send: s.SendMarkdown, getter: s}
		},
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
	value, ok := field.Interface().(*string)
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

	statuses   []statusResult
	statusErr  error
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

func (c *stubBotClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++
	if c.statusErr != nil {
		return nil, c.statusErr
	}
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

func TestClassifyReplyState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state string
		want  replyOutcome
	}{
		{"handoff completed is the only success", "handoff_completed", replyOutcomeHandoffCompleted},
		{"handoff completed tolerates case and padding", "  HANDOFF_COMPLETED  ", replyOutcomeHandoffCompleted},
		{"failed is terminal failure", "failed", replyOutcomeFailed},
		{"failed tolerates case and padding", "  FAILED ", replyOutcomeFailed},
		{"queued is in flight", "queued", replyOutcomeInFlight},
		{"preparing is in flight", "preparing", replyOutcomeInFlight},
		{"prepared is in flight", "prepared", replyOutcomeInFlight},
		{"sending is in flight", "sending", replyOutcomeInFlight},
		{"outcome_unknown is unknown", "outcome_unknown", replyOutcomeUnknown},
		{"empty state is unknown", "", replyOutcomeUnknown},
		{"unrecognized state is unknown", "totally-new-state", replyOutcomeUnknown},
		{"legacy delivered is no longer success", "delivered", replyOutcomeUnknown},
		{"legacy sent is no longer success", "sent", replyOutcomeUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyReplyState(tc.state))
		})
	}
}

func TestReplyHandoffStatusResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		state       string
		wantOutcome replyOutcome
		wantErr     func(error) bool
	}{
		{"handoff completed succeeds", "handoff_completed", replyOutcomeHandoffCompleted, nil},
		{"failed is terminal failure", "failed", replyOutcomeFailed, isReplyStatusFailed},
		{"queued keeps polling", "queued", replyOutcomeInFlight, nil},
		{"sending keeps polling", "sending", replyOutcomeInFlight, nil},
		{"outcome_unknown is unknown", "outcome_unknown", replyOutcomeUnknown, isReplyOutcomeUnknown},
		{"unrecognized state is unknown", "brand-new", replyOutcomeUnknown, isReplyOutcomeUnknown},
		{"empty state is unknown", "", replyOutcomeUnknown, isReplyOutcomeUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := replyHandoffStatusResult("r-1", &iris.ReplyStatusSnapshot{State: tc.state})
			assert.Equal(t, tc.wantOutcome, outcome)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, tc.wantErr(err), "unexpected error kind: %v", err)
			assert.False(t, isReplyOutcomeUnknown(err) && isReplyStatusFailed(err), "unknown and failed must stay disjoint")
		})
	}

	detail := "callback failed"
	_, err := replyHandoffStatusResult("r-2", &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iris reply r-2 failed: callback failed")
}

func TestCheckReplyHandoffStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("getter error stays in flight so polling can retry, never success", func(t *testing.T) {
		cause := errors.New("boom")
		outcome, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{err: cause}, "r")
		assert.Equal(t, replyOutcomeInFlight, outcome)
		require.Error(t, err)
		require.ErrorIs(t, err, cause)
	})

	t.Run("nil status is outcome unknown, never success", func(t *testing.T) {
		outcome, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{snap: nil}, "r")
		assert.Equal(t, replyOutcomeUnknown, outcome)
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
	})

	t.Run("handoff completed succeeds", func(t *testing.T) {
		getter := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}
		outcome, err := checkReplyHandoffStatus(ctx, getter, "r")
		assert.Equal(t, replyOutcomeHandoffCompleted, outcome)
		require.NoError(t, err)
	})

	t.Run("failed status returns failed error", func(t *testing.T) {
		outcome, err := checkReplyHandoffStatus(ctx, &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}, "r")
		assert.Equal(t, replyOutcomeFailed, outcome)
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
		assert.False(t, isReplyOutcomeUnknown(err))
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

	t.Run("completes on first handoff_completed", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}
		require.NoError(t, waitForReplyHandoff(context.Background(), g, "r"))
		assert.Equal(t, 1, g.calls)
	})

	t.Run("failed status returns failed error", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "failed"}}
		err := waitForReplyHandoff(context.Background(), g, "r")
		require.Error(t, err)
		assert.True(t, isReplyStatusFailed(err))
		assert.False(t, isReplyOutcomeUnknown(err))
	})

	t.Run("transient getter errors keep polling until the deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()

		g := &stubStatusGetter{err: errors.New("status down")}
		err := waitForReplyHandoff(ctx, g, "r")
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, err.Error(), "status down", "the last query error must survive into the unknown result")
		assert.Greater(t, g.calls, 1, "a single transient query error must not end polling")
	})

	t.Run("a transient getter error followed by handoff_completed succeeds", func(t *testing.T) {
		g := &stubStatusGetter{err: errors.New("blip")}
		g.onCall = func(call int) {
			if call >= 2 {
				g.err = nil
				g.snap = &iris.ReplyStatusSnapshot{State: "handoff_completed"}
			}
		}

		require.NoError(t, waitForReplyHandoff(context.Background(), g, "r"))
		assert.Equal(t, 2, g.calls)
	})

	t.Run("nil status ends polling as outcome unknown", func(t *testing.T) {
		g := &stubStatusGetter{snap: nil}
		err := waitForReplyHandoff(context.Background(), g, "r")
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
		assert.Equal(t, 1, g.calls)
	})

	t.Run("context cancellation while in flight is outcome unknown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		g := &stubStatusGetter{
			snap:   &iris.ReplyStatusSnapshot{State: "queued"},
			onCall: func(int) { cancel() },
		}
		err := waitForReplyHandoff(ctx, g, "r")
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("deadline exceeded while in flight is outcome unknown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "sending"}}
		err := waitForReplyHandoff(ctx, g, "r")
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestWaitForAcceptedReplyHandoff(t *testing.T) {
	t.Parallel()

	t.Run("nil accepted is outcome unknown, never success", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}
		err := waitForAcceptedReplyHandoff(context.Background(), g, nil)
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
		assert.Equal(t, 0, g.calls)
	})

	t.Run("blank request id is outcome unknown, never success", func(t *testing.T) {
		g := &stubStatusGetter{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}
		err := waitForAcceptedReplyHandoff(context.Background(), g, &iris.ReplyAcceptedResponse{RequestID: "   "})
		require.Error(t, err)
		assert.True(t, isReplyOutcomeUnknown(err))
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

func TestSendReply(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	for laneName, newLane := range replyLanesUnderTest() {
		t.Run(laneName+" lane", func(t *testing.T) {
			t.Run("non transport accept error is returned without re-post", func(t *testing.T) {
				s := &stubAcceptedSender{acceptErr: errors.New("nope")}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.Equal(t, 1, s.acceptCalls, "rejected admission must not be re-posted")
				assert.Equal(t, 0, s.calls)
			})

			t.Run("lost admission response is re-posted once then reported unknown", func(t *testing.T) {
				s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.True(t, isReplyOutcomeUnknown(err))
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
				assert.True(t, isReplyOutcomeUnknown(err),
					"the lost first attempt may already have been admitted, so the rejection is not authoritative")
				assert.Equal(t, replyAdmissionMaxAttempts, s.acceptCalls)
			})

			t.Run("lost admission response without a stable id is not re-posted", func(t *testing.T) {
				s := &stubAcceptedSender{acceptErr: lostAdmissionResponseError()}
				err := sendReply(ctx, newLane(s), "room", "msg", "", nil)
				require.Error(t, err)
				assert.True(t, isReplyOutcomeUnknown(err))
				assert.Equal(t, 1, s.acceptCalls)
			})

			t.Run("accepted then handoff_completed succeeds", func(t *testing.T) {
				s := &stubAcceptedSender{
					accepted: &iris.ReplyAcceptedResponse{RequestID: "r-1"},
					snap:     &iris.ReplyStatusSnapshot{State: "handoff_completed"},
				}
				require.NoError(t, sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil))
				assert.Equal(t, 1, s.acceptCalls)
			})

			t.Run("accepted then failed is terminal without re-post", func(t *testing.T) {
				detail := "boom"
				s := &stubAcceptedSender{
					accepted: &iris.ReplyAcceptedResponse{RequestID: "r-1"},
					snap:     &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail},
				}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.True(t, isReplyStatusFailed(err))
				assert.Equal(t, 1, s.acceptCalls, "failed is terminal and must not be re-posted")
			})

			t.Run("accepted then outcome_unknown does not re-post", func(t *testing.T) {
				s := &stubAcceptedSender{
					accepted: &iris.ReplyAcceptedResponse{RequestID: "r-1"},
					snap:     &iris.ReplyStatusSnapshot{State: "outcome_unknown"},
				}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.True(t, isReplyOutcomeUnknown(err))
				assert.Equal(t, 1, s.acceptCalls, "unknown outcome must not be re-posted")
			})

			t.Run("blank request id is outcome unknown without re-post", func(t *testing.T) {
				s := &stubAcceptedSender{accepted: &iris.ReplyAcceptedResponse{RequestID: ""}}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.True(t, isReplyOutcomeUnknown(err))
				assert.Equal(t, 1, s.acceptCalls)
				assert.Equal(t, 0, s.calls)
			})
		})
	}
}

func TestSendReplyConflictReissue(t *testing.T) {
	t.Parallel()

	for laneName, newLane := range replyLanesUnderTest() {
		t.Run(laneName+" lane", func(t *testing.T) {
			testSendReplyConflictReissue(t, newLane)
		})
	}
}

func testSendReplyConflictReissue(t *testing.T, newLane func(*stubAcceptedSender) replyLane) {
	t.Helper()

	ctx := context.Background()
	const clientRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("failed conflict reissues once and succeeds", func(t *testing.T) {
		s := &stubAcceptedSender{
			snap:      &iris.ReplyStatusSnapshot{State: "handoff_completed"},
			acceptErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed),
		}
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
		s := &stubAcceptedSender{acceptErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}

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
		s := &stubAcceptedSender{acceptErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}
		s.onAccept = func(call int) {
			if call == 2 {
				s.acceptErr = irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDAlreadyExists)
			}
		}

		err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDAlreadyExists, iris.HTTPErrorCode(err))
		require.Len(t, s.optsByAttempt, 2)
		last, _ := capturedSendOptions(t, s.optsByAttempt[1])
		assert.Equal(t, clientRequestID+":r1", last)
	})

	t.Run("other conflicts remain terminal without reissue", func(t *testing.T) {
		for _, code := range []string{
			iris.HTTPErrorCodeClientRequestIDOutcomeUnknown,
			iris.HTTPErrorCodeClientRequestIDPayloadMismatch,
			iris.HTTPErrorCodeClientRequestIDAlreadyExists,
			"IRIS_FUTURE_CONFLICT",
			"",
		} {
			t.Run(code, func(t *testing.T) {
				s := &stubAcceptedSender{acceptErr: irisConflictWithCode(code)}
				err := sendReply(ctx, newLane(s), "room", "msg", clientRequestID, nil)
				require.Error(t, err)
				assert.Equal(t, code, iris.HTTPErrorCode(err))
				assert.Equal(t, 1, s.acceptCalls)
			})
		}
	})

	t.Run("failed conflict after a lost attempt advances generation", func(t *testing.T) {
		s := &stubAcceptedSender{
			snap:      &iris.ReplyStatusSnapshot{State: "handoff_completed"},
			acceptErr: lostAdmissionResponseError(),
		}
		s.onAccept = func(call int) {
			switch call {
			case 2:
				s.acceptErr = irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)
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
		err := tr.SendMessage(inboundCtx(ctx), "room-x", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "iris down")
		assert.Equal(t, 1, c.acceptCalls)
	})

	t.Run("success on first attempt", func(t *testing.T) {
		c := &stubBotClient{statuses: []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}}}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, "hi", c.lastMessage)
	})

	t.Run("failed status is terminal and is never re-posted", func(t *testing.T) {
		detail := "cb failed"
		c := &stubBotClient{statuses: []statusResult{
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
			{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}},
		}}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(inboundCtx(ctx), "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cb failed")
		assert.Equal(t, 1, c.acceptCalls, "explicit iris failure must not trigger a second POST")
	})

	t.Run("failed status surfaces the wrapped failed error", func(t *testing.T) {
		detail := "cb failed"
		c := &stubBotClient{statuses: []statusResult{
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
		}}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(inboundCtx(ctx), "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "cb failed")
		assert.False(t, errors.Is(err, ErrReplyOutcomeUnknown))
		assert.Equal(t, 1, c.acceptCalls)
	})

	t.Run("persistent status query errors surface outcome unknown to the caller", func(t *testing.T) {
		deadlineCtx, cancel := context.WithTimeout(inboundCtx(ctx), 700*time.Millisecond)
		defer cancel()

		c := &stubBotClient{statusErr: errors.New("status down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendMessage(deadlineCtx, "room", "hi")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		assert.Equal(t, 1, c.acceptCalls, "unknown outcome must not trigger a second POST")
	})

	t.Run("thread id in context adds an extra send option", func(t *testing.T) {
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(WithThreadID(inboundCtx(ctx), "t-1"), "room", "hi"))
		assert.Equal(t, 2, c.lastOptsLen)
	})

	t.Run("no thread id yields a single client-request-id option", func(t *testing.T) {
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil)
		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.lastOptsLen)
	})

	t.Run("without an inbound identity no client request id is attached", func(t *testing.T) {
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

	ctx := context.Background()
	const wantRequestID = "hololive:v1:message:m-1:reply:0"

	t.Run("markdown lane sends through SendMarkdown with the stable request id", func(t *testing.T) {
		c := &stubBotClient{statuses: []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}}}
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
	})

	t.Run("regular chat renders unicode on the accepted lane", func(t *testing.T) {
		c := &stubBotClient{statuses: []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}}}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		ctx := WithRoomChat(inboundCtx(ctx), "MultiChat", "")
		require.NoError(t, tr.SendMessage(ctx, "room-reg", "## **hi**"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, "【𝗵𝗶】", c.lastMessage)
	})

	t.Run("disabled flag keeps the accepted lane", func(t *testing.T) {
		c := &stubBotClient{statuses: []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}}}}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(false))

		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "**hi**"))
		assert.Equal(t, 1, c.acceptCalls)
		assert.Equal(t, 0, c.markdownCalls)
		assert.Equal(t, "𝗵𝗶", c.lastMessage)

		requestID, _ := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, wantRequestID, requestID)
	})

	t.Run("failed handoff is terminal on the markdown lane", func(t *testing.T) {
		detail := "cb failed"
		c := &stubBotClient{statuses: []statusResult{
			{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}},
			{snap: &iris.ReplyStatusSnapshot{State: "handoff_completed"}},
		}}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		err := tr.SendMessage(inboundCtx(ctx), "room", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "cb failed")
		assert.Equal(t, 1, c.markdownCalls)
		require.Len(t, c.optsByAttempt, 1)

		requestID, _ := capturedSendOptions(t, c.optsByAttempt[0])
		assert.Equal(t, wantRequestID, requestID)
	})

	t.Run("send error returns after a single markdown attempt", func(t *testing.T) {
		c := &stubBotClient{markdownErr: errors.New("iris down")}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		err := tr.SendMessage(inboundCtx(ctx), "room-x", "hi")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send message: ")
		assert.Contains(t, err.Error(), "iris down")
		assert.Equal(t, 1, c.markdownCalls)
	})

	t.Run("lost admission response is re-posted with the same request id", func(t *testing.T) {
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
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, WithMarkdownReplies(true))

		require.NoError(t, tr.SendMessage(WithThreadID(inboundCtx(ctx), "t-1"), "room", "hi"))
		assert.Equal(t, 2, c.lastOptsLen)

		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Equal(t, wantRequestID, requestID)
	})

	t.Run("nil option is ignored", func(t *testing.T) {
		c := &stubBotClient{}
		tr := NewCommandTransport(c, nil, nil, WithMarkdownReplies(true))

		require.NoError(t, tr.SendMessage(inboundCtx(ctx), "room", "hi"))
		assert.Equal(t, 1, c.markdownCalls)
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

	t.Run("client error is wrapped without room id", func(t *testing.T) {
		c := &stubBotClient{imageErr: errors.New("img down")}
		tr := NewCommandTransport(c, nil)
		err := tr.SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send image: ")
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
		assert.Contains(t, err.Error(), "send image: ")
		assert.Contains(t, err.Error(), "image lease last modified mismatch")
		assert.False(t, errors.Is(err, ErrReplyOutcomeUnknown))
	})

	t.Run("lost admission response does not advance the image generation", func(t *testing.T) {
		c := &stubBotClient{imageErr: lostAdmissionResponseError()}
		err := tr(c).SendImage(inboundCtx(ctx), "room", []byte("x"))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		require.Len(t, c.optsByAttempt, 1)
		requestID, _ := capturedSendOptions(t, c.optsByAttempt[0])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)
	})

	t.Run("failed conflict reissues with the same image payload", func(t *testing.T) {
		data := []byte("image-payload")
		c := &stubBotClient{imageErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}
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
		c := &stubBotClient{imageErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}

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
		c := &stubBotClient{imageErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDOutcomeUnknown)}

		err := tr(c).SendImage(inboundCtx(ctx), "room", []byte("image-payload"))
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown, iris.HTTPErrorCode(err))
		assert.Equal(t, 1, c.imageCalls)
	})

	t.Run("a plain rejection stays a terminal failure", func(t *testing.T) {
		c := &stubBotClient{imageErr: errors.New("img rejected")}
		err := tr(c).SendImage(ctx, "room", []byte("x"))
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrReplyOutcomeUnknown))
	})
}

func TestCommandTransportSendImages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("multiple images are sent as one album", func(t *testing.T) {
		c := &stubBotClient{}
		images := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

		require.NoError(t, tr(c).SendImages(ctx, "room-1", images))
		assert.Equal(t, "room-1", c.lastImageRoom)
		assert.Equal(t, images, c.lastImages)
		assert.Nil(t, c.lastImage)
	})

	t.Run("single image preserves the single image lane", func(t *testing.T) {
		c := &stubBotClient{}

		require.NoError(t, tr(c).SendImages(ctx, "room-1", [][]byte{[]byte("one")}))
		assert.Equal(t, []byte("one"), c.lastImage)
		assert.Nil(t, c.lastImages)
	})

	t.Run("thread and stable request id are forwarded, one ordinal per emission", func(t *testing.T) {
		c := &stubBotClient{}
		images := [][]byte{[]byte("one"), []byte("two")}
		ctx := WithReplyIdentity(WithThreadID(ctx, "t-1"), "message:m-1")

		require.NoError(t, tr(c).SendImages(ctx, "room-1", images))
		assert.Equal(t, 2, c.lastOptsLen)
		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)

		require.NoError(t, tr(c).SendImages(ctx, "room-1", images))
		requestID, _ = capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "hololive:v1:message:m-1:reply:1", requestID,
			"a second emission inside one inbound message must not reuse the first ordinal")
	})

	t.Run("without an inbound identity no client request id is attached", func(t *testing.T) {
		c := &stubBotClient{}

		require.NoError(t, tr(c).SendImages(WithThreadID(ctx, "t-1"), "room-1", [][]byte{[]byte("one"), []byte("two")}))
		requestID, threadID := capturedSendOptions(t, c.lastOpts)
		assert.Equal(t, "t-1", threadID)
		assert.Empty(t, requestID)
	})

	t.Run("client error is wrapped without room id", func(t *testing.T) {
		c := &stubBotClient{multiErr: errors.New("album down")}

		err := tr(c).SendImages(ctx, "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send images: ")
		assert.Contains(t, err.Error(), "album down")
		assert.False(t, errors.Is(err, ErrReplyOutcomeUnknown))
	})

	t.Run("lost admission response does not advance the album generation", func(t *testing.T) {
		c := &stubBotClient{multiErr: lostAdmissionResponseError()}

		err := tr(c).SendImages(inboundCtx(ctx), "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrReplyOutcomeUnknown)
		require.Len(t, c.optsByAttempt, 1)
		requestID, _ := capturedSendOptions(t, c.optsByAttempt[0])
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", requestID)
	})

	t.Run("failed conflict reissues with the same album payload", func(t *testing.T) {
		images := [][]byte{[]byte("one"), []byte("two")}
		c := &stubBotClient{multiErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}
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

	t.Run("failed conflict exhausts two album reissue generations", func(t *testing.T) {
		c := &stubBotClient{multiErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDFailed)}

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
		c := &stubBotClient{multiErr: irisConflictWithCode(iris.HTTPErrorCodeClientRequestIDPayloadMismatch)}

		err := tr(c).SendImages(inboundCtx(ctx), "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Equal(t, iris.HTTPErrorCodeClientRequestIDPayloadMismatch, iris.HTTPErrorCode(err))
		assert.Equal(t, 1, c.multiImageCalls)
	})

	t.Run("failed reply status is wrapped with detail", func(t *testing.T) {
		detail := "album lease last modified mismatch"
		c := &stubBotClient{
			multiAccepted: &iris.ReplyAcceptedResponse{RequestID: "r-images"},
			statuses:      []statusResult{{snap: &iris.ReplyStatusSnapshot{State: "failed", Detail: &detail}}},
		}

		err := tr(c).SendImages(ctx, "room", [][]byte{[]byte("one"), []byte("two")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "send images: ")
		assert.Contains(t, err.Error(), "album lease last modified mismatch")
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
		formatter := messageformatter.NewResponseFormatter("!", nil)
		transport := NewCommandTransport(c, formatter)
		require.NoError(t, transport.SendError(ctx, "room", "totally_unknown_key"))
		assert.Equal(t, messagestrings.FallbackSentinel, c.lastMessage)
	})
}

func tr(c iris.BotClient) *CommandTransport {
	return NewCommandTransport(c, nil)
}

func inboundCtx(ctx context.Context) context.Context {
	return WithRoomChat(WithReplyIdentity(ctx, "message:m-1"), "OM", "")
}
