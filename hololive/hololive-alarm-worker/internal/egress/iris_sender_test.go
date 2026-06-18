package egress

import (
	"context"
	"testing"

	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type irisSenderTestClient struct {
	karingRequests []iris.KaringContentListRequest
}

func (c *irisSenderTestClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
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
