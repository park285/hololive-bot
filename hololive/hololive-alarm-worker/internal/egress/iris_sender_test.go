package egress

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	richTextCalls       []irisSenderTestCall
	markdownCalls       []irisSenderTestCall
	textAccepted        *iris.ReplyAcceptedResponse
	textAcceptedSet     bool
	richTextErr         error
	richTextAccepted    *iris.ReplyAcceptedResponse
	richTextAcceptedSet bool
}

func (c *irisSenderTestClient) SendMessageAccepted(_ context.Context, roomID, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.textCalls = append(c.textCalls, irisSenderTestCall{roomID: roomID, message: message, opts: len(opts)})
	if c.textAcceptedSet {
		return c.textAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: irisSenderQueued, RequestID: "text-request-1"}, nil
}

func (c *irisSenderTestClient) SendRichText(_ context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.richTextCalls = append(c.richTextCalls, irisSenderTestCall{roomID: roomID, message: markdown, opts: len(opts)})
	if c.richTextErr != nil {
		return nil, c.richTextErr
	}

	if c.richTextAcceptedSet {
		return c.richTextAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: irisSenderQueued, RequestID: "markdown-request-1"}, nil
}

func (c *irisSenderTestClient) SendMarkdown(_ context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.markdownCalls = append(c.markdownCalls, irisSenderTestCall{roomID: roomID, message: markdown, opts: len(opts)})
	if c.richTextAcceptedSet {
		return c.richTextAccepted, nil
	}

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: irisSenderQueued, RequestID: "markdown-request-1"}, nil
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
	require.Len(t, client.richTextCalls, 1)
	assert.Equal(t, testIrisSenderRoomID, client.richTextCalls[0].roomID)
	assert.Equal(t, "**hello**", client.richTextCalls[0].message)
	assert.Equal(t, 1, client.richTextCalls[0].opts)
}

func TestIrisMessageSenderRendersPlainTextForRegularChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: "regular"}),
	)

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "## **hello**"))

	assert.Empty(t, client.richTextCalls)
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

	assert.Empty(t, client.richTextCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
}

func TestIrisMessageSenderRendersPlainTextForUnknownRoom(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithMarkdownRoomChat(staticRooms{}))

	require.NoError(t, sendPreparedTestMessage(t.Context(), sender, "**hello**"))

	assert.Empty(t, client.richTextCalls)
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
	require.Len(t, client.richTextCalls, 1)
	assert.Equal(t, 1, client.richTextCalls[0].opts)
}

func TestIrisMessageSenderMarkdownWrapsError(t *testing.T) {
	client := &irisSenderTestClient{richTextErr: errors.New("boom")}
	sender := NewIrisMessageSender(
		client,
		WithMarkdownReplies(true),
		WithMarkdownRoomChat(staticRooms{testIrisSenderRoomID: testIrisSenderOpenRoomKind}),
	)

	err := sendPreparedTestMessage(t.Context(), sender, "**hello**")

	require.ErrorContains(t, err, "iris send message")
	require.ErrorIs(t, err, client.richTextErr)
}

func TestIrisMessageSenderGuardsNilInputs(t *testing.T) {
	sender := NewIrisMessageSender(nil)

	require.ErrorContains(t, sendPreparedTestMessage(t.Context(), sender, "hello"), "client is nil")
	require.ErrorContains(t, sendPreparedTestMessage(t.Context(), sender, "hello", "req-1"), "client is nil")
}
