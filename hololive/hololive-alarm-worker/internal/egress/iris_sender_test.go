package egress

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIrisSenderRoomID       = "room-1"
	testIrisSenderOpenRoomKind = "open"
)

type irisSenderTestCall struct {
	roomID  string
	message string
	opts    int
}

type irisSenderTestClient struct {
	karingRequests []iris.KaringContentListRequest
	textCalls      []irisSenderTestCall
	markdownCalls  []irisSenderTestCall
	statusCalls    int
	textErr        error
	markdownErr    error
	karingErr      error
	accepted       *iris.KaringDryRunResponse
	acceptedSet    bool
	getStatus      func(int, string) (*iris.ReplyStatusSnapshot, error)
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

	return &iris.ReplyAcceptedResponse{}, nil
}

func (c *irisSenderTestClient) SendKaringContentList(_ context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	c.karingRequests = append(c.karingRequests, req)
	if c.karingErr != nil {
		return nil, c.karingErr
	}

	if c.acceptedSet {
		return c.accepted, nil
	}

	return acceptedKaringTestResponse("karing-request-1"), nil
}

func (c *irisSenderTestClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.statusCalls++
	if c.getStatus != nil {
		return c.getStatus(c.statusCalls, requestID)
	}

	return karingTestStatus(requestID, "handoff_completed"), nil
}

func acceptedKaringTestResponse(requestID string) *iris.KaringDryRunResponse {
	return &iris.KaringDryRunResponse{Success: true, Delivery: "queued", RequestID: requestID}
}

func karingTestStatus(requestID, state string) *iris.ReplyStatusSnapshot {
	return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}
}

type staticRooms map[string]string

func (rooms staticRooms) OpenChat(_ context.Context, roomID string) bool {
	return rooms[roomID] == testIrisSenderOpenRoomKind
}

func (rooms staticRooms) RegularChat(_ context.Context, roomID string) bool {
	return rooms[roomID] == "regular"
}

func TestIrisMessageSenderPreservesReceiverRoomID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)

	err := sender.SendKaringContentList(t.Context(), "464252100463241", &iris.KaringContentListRequest{
		ReceiverRoomID: 464252100463241,
	})

	require.NoError(t, err)
	require.Len(t, client.karingRequests, 1)
	assert.Equal(t, int64(464252100463241), client.karingRequests[0].ReceiverRoomID)
	assert.Empty(t, client.karingRequests[0].ReceiverName)
}

func TestIrisMessageSenderFallsBackToReceiverNameWithoutMutatingRequest(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)
	request := &iris.KaringContentListRequest{}

	err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, request)

	require.NoError(t, err)
	require.Len(t, client.karingRequests, 1)
	assert.Equal(t, testIrisSenderRoomID, client.karingRequests[0].ReceiverName)
	assert.Zero(t, client.karingRequests[0].ReceiverRoomID)
	assert.Empty(t, request.ReceiverName)
}

func TestIrisMessageSenderPreservesKaringClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)
	clientRequestID := "hololive-alarm:request-1"

	err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{
		ClientRequestID: &clientRequestID,
	})

	require.NoError(t, err)
	require.Len(t, client.karingRequests, 1)
	require.NotNil(t, client.karingRequests[0].ClientRequestID)
	assert.Equal(t, clientRequestID, *client.karingRequests[0].ClientRequestID)
}

func TestIrisMessageSenderUsesMarkdownLaneForOpenChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sender.SendMessage(t.Context(), testIrisSenderRoomID, "**hello**"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, testIrisSenderRoomID, client.markdownCalls[0].roomID)
	assert.Equal(t, "**hello**", client.markdownCalls[0].message)
	assert.Zero(t, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderRendersPlainTextForRegularChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithRoomChat(staticRooms{testIrisSenderRoomID: "regular"}),
	)

	require.NoError(t, sender.SendMessage(t.Context(), testIrisSenderRoomID, "## **hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, testIrisSenderRoomID, client.textCalls[0].roomID)
	assert.Equal(t, "【𝗵𝗲𝗹𝗹𝗼】", client.textCalls[0].message)
	assert.Zero(t, client.textCalls[0].opts)
}

func TestIrisMessageSenderRendersPlainTextForOpenChatWhenMarkdownDisabled(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(false),
		WithRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sender.SendMessage(t.Context(), testIrisSenderRoomID, "**hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
}

func TestIrisMessageSenderRendersPlainTextForUnknownRoom(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithRoomChat(staticRooms{}))

	require.NoError(t, sender.SendMessage(t.Context(), testIrisSenderRoomID, "**hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
}

func TestIrisMessageSenderRegularChatRequiresPositiveRoomFact(t *testing.T) {
	sender := NewIrisMessageSender(&irisSenderTestClient{}, WithRoomChat(staticRooms{
		"open-room":    testIrisSenderOpenRoomKind,
		"regular-room": "regular",
	}))

	assert.True(t, sender.RegularChat(t.Context(), "regular-room"))
	assert.False(t, sender.RegularChat(t.Context(), "open-room"))
	assert.False(t, sender.RegularChat(t.Context(), "missing-room"))
}

func TestIrisMessageSenderPlainTextPropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), testIrisSenderRoomID, "hello", "req-1"))

	require.Len(t, client.textCalls, 1)
	assert.Equal(t, 1, client.textCalls[0].opts)
}

func TestIrisMessageSenderMarkdownPropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), testIrisSenderRoomID, "**hello**", "req-1"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, 1, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderMarkdownWrapsError(t *testing.T) {
	client := &irisSenderTestClient{markdownErr: errors.New("boom")}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	err := sender.SendMessage(t.Context(), testIrisSenderRoomID, "**hello**")

	require.ErrorContains(t, err, "iris send message")
	require.ErrorIs(t, err, client.markdownErr)
}

func TestIrisMessageSenderWaitsThroughEveryInFlightState(t *testing.T) {
	states := []string{"queued", "preparing", "prepared", "sending", "handoff_completed"}
	client := &irisSenderTestClient{
		getStatus: func(call int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			return karingTestStatus(requestID, states[call-1]), nil
		},
	}
	sender := NewIrisMessageSender(client)

	sender.karingStatusPollInterval = time.Nanosecond

	err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{})

	require.NoError(t, err)
	assert.Equal(t, len(states), client.statusCalls)
	assert.Len(t, client.karingRequests, 1)
}

func TestIrisMessageSenderRetriesStatusObservationWithoutReposting(t *testing.T) {
	client := &irisSenderTestClient{
		getStatus: func(call int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			if call == 1 {
				return nil, errors.New("temporary status error")
			}

			return karingTestStatus(requestID, "handoff_completed"), nil
		},
	}
	sender := NewIrisMessageSender(client)

	sender.karingStatusPollInterval = time.Nanosecond

	err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{})

	require.NoError(t, err)
	assert.Equal(t, 2, client.statusCalls)
	assert.Len(t, client.karingRequests, 1)
}

func TestIrisMessageSenderRejectsInvalidAdmissionAsOutcomeUnknown(t *testing.T) {
	testCases := []struct {
		name     string
		accepted *iris.KaringDryRunResponse
	}{
		{name: "empty response"},
		{name: "not successful", accepted: &iris.KaringDryRunResponse{Delivery: "queued", RequestID: "request-1"}},
		{name: "not queued", accepted: &iris.KaringDryRunResponse{Success: true, Delivery: "sending", RequestID: "request-1"}},
		{name: "missing request id", accepted: &iris.KaringDryRunResponse{Success: true, Delivery: "queued"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &irisSenderTestClient{accepted: tc.accepted, acceptedSet: true}
			sender := NewIrisMessageSender(client)

			err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{})

			require.ErrorIs(t, err, ErrKaringOutcomeUnknown)
			assert.Zero(t, client.statusCalls)
			assert.Len(t, client.karingRequests, 1)
		})
	}
}

func TestIrisMessageSenderClassifiesTerminalStatus(t *testing.T) {
	testCases := []struct {
		name   string
		status *iris.ReplyStatusSnapshot
		want   error
	}{
		{name: "failed", status: karingTestStatus("karing-request-1", "failed"), want: ErrKaringStatusFailed},
		{name: "outcome unknown", status: karingTestStatus("karing-request-1", "outcome_unknown"), want: ErrKaringOutcomeUnknown},
		{name: "unknown state", status: karingTestStatus("karing-request-1", "mystery"), want: ErrKaringOutcomeUnknown},
		{name: "empty status", want: ErrKaringOutcomeUnknown},
		{name: "request mismatch", status: karingTestStatus("another-request", "handoff_completed"), want: ErrKaringOutcomeUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &irisSenderTestClient{
				getStatus: func(_ int, _ string) (*iris.ReplyStatusSnapshot, error) {
					return tc.status, nil
				},
			}
			sender := NewIrisMessageSender(client)

			err := sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{})

			require.ErrorIs(t, err, tc.want)
			assert.Equal(t, 1, client.statusCalls)
			assert.Len(t, client.karingRequests, 1)
		})
	}
}

func TestIrisMessageSenderPollingDeadlineIsOutcomeUnknown(t *testing.T) {
	client := &irisSenderTestClient{
		getStatus: func(_ int, requestID string) (*iris.ReplyStatusSnapshot, error) {
			return nil, fmt.Errorf("GET /reply-status/%s failed", requestID)
		},
	}
	sender := NewIrisMessageSender(client)

	sender.karingStatusPollInterval = time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)

	defer cancel()

	err := sender.SendKaringContentList(ctx, testIrisSenderRoomID, &iris.KaringContentListRequest{})
	if err == nil {
		t.Fatal("SendKaringContentList() error = nil, want outcome unknown")
	}

	require.ErrorIs(t, err, ErrKaringOutcomeUnknown)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotContains(t, err.Error(), "karing-request-1")
	assert.GreaterOrEqual(t, client.statusCalls, 1)
	assert.Len(t, client.karingRequests, 1)
}

func TestIrisMessageSenderGuardsNilInputs(t *testing.T) {
	sender := NewIrisMessageSender(nil)

	require.ErrorContains(t, sender.SendMessage(t.Context(), testIrisSenderRoomID, "hello"), "client is nil")
	require.ErrorContains(t, sender.SendMessageWithClientRequestID(t.Context(), testIrisSenderRoomID, "hello", "req-1"), "client is nil")
	require.ErrorContains(t, sender.SendKaringContentList(t.Context(), testIrisSenderRoomID, &iris.KaringContentListRequest{}), "client is nil")

	client := &irisSenderTestClient{}
	require.ErrorContains(t, NewIrisMessageSender(client).SendKaringContentList(t.Context(), testIrisSenderRoomID, nil), "request is nil")
}
