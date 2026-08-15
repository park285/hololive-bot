package egress

import (
	"context"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	textErr        error
	markdownErr    error
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

func (c *irisSenderTestClient) SendKaringContentList(_ context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	c.karingRequests = append(c.karingRequests, *req)
	return &iris.KaringDryRunResponse{}, nil
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
	assert.Zero(t, client.karingRequests[0].ReceiverName)
}

func TestIrisMessageSenderFallsBackToReceiverName(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)

	err := sender.SendKaringContentList(t.Context(), "room-1", &iris.KaringContentListRequest{})

	require.NoError(t, err)
	require.Len(t, client.karingRequests, 1)
	assert.Equal(t, "room-1", client.karingRequests[0].ReceiverName)
	assert.Zero(t, client.karingRequests[0].ReceiverRoomID)
}

func TestIrisMessageSenderPreservesKaringClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)
	clientRequestID := "hololive-alarm:request-1"

	err := sender.SendKaringContentList(t.Context(), "room-1", &iris.KaringContentListRequest{
		ClientRequestID: &clientRequestID,
	})

	require.NoError(t, err)
	require.Len(t, client.karingRequests, 1)
	require.NotNil(t, client.karingRequests[0].ClientRequestID)
	assert.Equal(t, clientRequestID, *client.karingRequests[0].ClientRequestID)
}

type staticRooms map[string]bool

func (s staticRooms) OpenChat(_ context.Context, roomID string) bool {
	return s[roomID]
}

func TestIrisMessageSenderUsesMarkdownLaneWhenEnabled(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithRoomChat(staticRooms{"room-1": true}))

	require.NoError(t, sender.SendMessage(t.Context(), "room-1", "**hello**"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, "room-1", client.markdownCalls[0].roomID)
	assert.Equal(t, "**hello**", client.markdownCalls[0].message)
	assert.Zero(t, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderUsesPlainLaneForRegularChat(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithRoomChat(staticRooms{"room-1": false}))

	require.NoError(t, sender.SendMessage(t.Context(), "room-1", "## **hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "【𝗵𝗲𝗹𝗹𝗼】", client.textCalls[0].message)
}

func TestIrisMessageSenderUsesTextLaneWhenDisabled(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(false))

	require.NoError(t, sender.SendMessage(t.Context(), "room-1", "**hello**"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, "room-1", client.textCalls[0].roomID)
	assert.Equal(t, "𝗵𝗲𝗹𝗹𝗼", client.textCalls[0].message)
	assert.Zero(t, client.textCalls[0].opts)
}

func TestIrisMessageSenderDefaultsToTextLane(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client)

	require.NoError(t, sender.SendMessage(t.Context(), "room-1", "hello"))

	assert.Empty(t, client.markdownCalls)
	assert.Len(t, client.textCalls, 1)
}

func TestIrisMessageSenderMarkdownLanePropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithRoomChat(staticRooms{"room-1": true}))

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "**hello**", "req-1"))

	assert.Empty(t, client.textCalls)
	require.Len(t, client.markdownCalls, 1)
	assert.Equal(t, "room-1", client.markdownCalls[0].roomID)
	assert.Equal(t, "**hello**", client.markdownCalls[0].message)
	assert.Equal(t, 1, client.markdownCalls[0].opts)
}

func TestIrisMessageSenderTextLanePropagatesClientRequestID(t *testing.T) {
	client := &irisSenderTestClient{}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(false))

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hello", "req-1"))

	assert.Empty(t, client.markdownCalls)
	require.Len(t, client.textCalls, 1)
	assert.Equal(t, 1, client.textCalls[0].opts)
}

func TestIrisMessageSenderMarkdownLaneWrapsError(t *testing.T) {
	client := &irisSenderTestClient{markdownErr: fmt.Errorf("boom")}
	sender := NewIrisMessageSender(client, WithMarkdownReplies(true), WithRoomChat(staticRooms{"room-1": true}))

	err := sender.SendMessageWithClientRequestID(t.Context(), "room-1", "**hello**", "req-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "iris send message: ")
	assert.Contains(t, err.Error(), "boom")
}

func TestIrisMessageSenderNilClientGuardsBothLanes(t *testing.T) {
	for _, markdown := range []bool{true, false} {
		sender := NewIrisMessageSender(nil, WithMarkdownReplies(markdown))

		require.ErrorContains(t, sender.SendMessage(t.Context(), "room-1", "hello"), "iris message sender: client is nil")
		require.ErrorContains(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hello", "req-1"), "iris message sender: client is nil")
		require.ErrorContains(t, sender.SendKaringContentList(t.Context(), "room-1", &iris.KaringContentListRequest{}), "iris message sender: client is nil")
	}
}

func TestIrisMessageSenderUnsupportedClientGuardsBothLanes(t *testing.T) {
	for _, markdown := range []bool{true, false} {
		sender := NewIrisMessageSender(struct{}{}, WithMarkdownReplies(markdown))

		require.ErrorContains(t, sender.SendMessage(t.Context(), "room-1", "hello"), "iris message sender: client is nil")
	}
}
