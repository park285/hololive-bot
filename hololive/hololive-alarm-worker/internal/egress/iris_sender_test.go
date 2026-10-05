package egress

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

const (
	irisSenderQueued           = "queued"
	testIrisSenderRoomID       = "room-1"
	testIrisSenderOpenRoomKind = "open"
)

type irisSenderTestCall struct {
	roomID  string
	message string
	opts    int
}

type irisSenderTestClient struct {
	textCalls           []irisSenderTestCall
	markdownCalls       []irisSenderTestCall
	statusCalls         int
	textErr             error
	markdownErr         error
	markdownAccepted    *iris.ReplyAcceptedResponse
	markdownAcceptedSet bool
	getStatus           func(int, string) (*iris.ReplyStatusSnapshot, error)
}

func (c *irisSenderTestClient) SendMessage(_ context.Context, roomID, message string, opts ...iris.SendOption) error {
	c.textCalls = append(c.textCalls, irisSenderTestCall{roomID: roomID, message: message, opts: len(opts)})
	return c.textErr
}

func (c *irisSenderTestClient) SendMarkdown(_ context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.markdownCalls = append(c.markdownCalls, irisSenderTestCall{roomID: roomID, message: markdown, opts: len(opts)})
	if c.markdownErr != nil {
		return nil, c.markdownErr
	}

	if c.markdownAcceptedSet {
		return c.markdownAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: irisSenderQueued, RequestID: "markdown-request-1"}, nil
}

func (c *irisSenderTestClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++
	if c.getStatus != nil {
		return c.getStatus(c.statusCalls, requestID)
	}

	return replyTestStatus(requestID, "handoff_completed"), nil
}

func replyTestStatus(requestID, state string) *iris.ReplyStatusSnapshot {
	return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}
}

type staticRooms map[string]string

func (rooms staticRooms) OpenChat(_ context.Context, roomID string) bool {
	return rooms[roomID] == testIrisSenderOpenRoomKind
}

func TestIrisMessageSenderUsesMarkdownLaneForOpenChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "**hello**"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, testIrisSenderRoomID, client.markdownCalls[0].roomID)
	assert.Equal(t, "**hello**", client.markdownCalls[0].message)
	assert.Equal(t, 1, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderRendersPlainTextForRegularChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: "regular"}),
	)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "## **hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, testIrisSenderRoomID, client.textCalls[0].roomID)
	assert.Equal(t, "【𝗵𝗲𝗹𝗹𝗼】", client.textCalls[0].message)
	assert.Equal(t, 1, client.textCalls[0].opts)
}

func TestIrisMessageSenderRendersPlainTextForOpenChatWhenMarkdownDisabled(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(false),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "**hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
}

func TestIrisMessageSenderRendersPlainTextForUnknownRoom(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{}))

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "**hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
}

func TestIrisMessageSenderPlainTextPropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "hello", "req-1"))

	require.Len(t, client.textCalls, 1)
	assert.Equal(t, 1, client.textCalls[0].opts)
}

func TestIrisMessageSenderMarkdownPropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "**hello**", "req-1"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, 1, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderMarkdownWrapsError(t *testing.T) {
	client := &irisSenderTestClient{markdownErr: errors.New("boom")}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	err := sendPreparedTestMessage(t.Context(), sender, "**hello**")

	require.ErrorContains(t, err, "iris send message")
	require.ErrorIs(t, err, client.markdownErr)
}

func newMarkdownTestSender(client *irisSenderTestClient) *IrisMessageSender {
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	sender.replyStatusPollInterval = time.Nanosecond

	return sender
}

func TestIrisMessageSenderWaitsThroughEveryInFlightState(t *testing.T) {
	states := []string{"queued", "preparing", "prepared", "sending", "handoff_completed"}
	client := &irisSenderTestClient{
		getStatus: func(call int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			return replyTestStatus(requestID, states[call-1]), nil
		},
	}
	sender := newMarkdownTestSender(client)

	err := sendPreparedTestMessage(t.Context(), sender, "**hello**")

	require.NoError(t, err)
	assert.Equal(t, len(states), client.statusCalls)
	assert.Len(t, client.markdownCalls, 1)
}

func TestIrisMessageSenderRetriesStatusObservationWithoutReposting(t *testing.T) {
	client := &irisSenderTestClient{
		getStatus: func(call int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			if call == 1 {
				return nil, errors.New("temporary status error")
			}

			return replyTestStatus(requestID, "handoff_completed"), nil
		},
	}
	sender := newMarkdownTestSender(client)

	err := sendPreparedTestMessage(t.Context(), sender, "**hello**")

	require.NoError(t, err)
	assert.Equal(t, 2, client.statusCalls)
	assert.Len(t, client.markdownCalls, 1)
}

func TestIrisMessageSenderClassifiesTerminalStatus(t *testing.T) {
	testCases := []struct {
		name   string
		status *iris.ReplyStatusSnapshot
		want   error
	}{
		{name: "failed", status: replyTestStatus("markdown-request-1", "failed"), want: sendoutcome.ErrHandoffFailed},
		{name: "outcome unknown", status: replyTestStatus("markdown-request-1", "outcome_unknown"), want: sendoutcome.ErrHandoffOutcomeUnknown},
		{name: "unknown state", status: replyTestStatus("markdown-request-1", "mystery"), want: sendoutcome.ErrHandoffOutcomeUnknown},
		{name: "empty status", want: sendoutcome.ErrHandoffOutcomeUnknown},
		{name: "request mismatch", status: replyTestStatus("another-request", "handoff_completed"), want: sendoutcome.ErrHandoffOutcomeUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &irisSenderTestClient{
				getStatus: func(_ int, _ string) (*iris.ReplyStatusSnapshot, error) {
					return tc.status, nil
				},
			}
			sender := newMarkdownTestSender(client)

			err := sendPreparedTestMessage(t.Context(), sender, "**hello**")

			require.ErrorIs(t, err, tc.want)
			assert.Equal(t, 1, client.statusCalls)
			assert.Len(t, client.markdownCalls, 1)
		})
	}
}

func TestIrisMessageSenderPollingDeadlineIsOutcomeUnknown(t *testing.T) {
	client := &irisSenderTestClient{
		getStatus: func(_ int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			return nil, fmt.Errorf("GET /reply-status/%s failed", requestID)
		},
	}
	sender := newMarkdownTestSender(client)

	sender.replyStatusPollInterval = time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)

	defer cancel()

	err := sendPreparedTestMessage(ctx, sender, "**hello**")
	if err == nil {
		t.Fatal("SendMessage() error = nil, want outcome unknown")
	}

	require.ErrorIs(t, err, sendoutcome.ErrHandoffOutcomeUnknown)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotContains(t, err.Error(), "markdown-request-1")
	assert.GreaterOrEqual(t, client.statusCalls, 1)
	assert.Len(t, client.markdownCalls, 1)
}

func TestIrisMessageSenderGuardsNilInputs(t *testing.T) {
	sender := NewIrisMessageSender(nil)

	require.ErrorContains(t, sendPreparedTestMessage(t.Context(), sender, "hello"), "client is nil")
	require.ErrorContains(t, sendPreparedTestMessage(t.Context(), sender, "hello", "req-1"), "client is nil")
}
