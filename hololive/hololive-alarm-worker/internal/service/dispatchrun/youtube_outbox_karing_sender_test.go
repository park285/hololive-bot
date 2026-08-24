package dispatchrun

import (
	"context"
	"testing"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type karingSenderStubIrisSender struct {
	sendMessageCalls []karingSenderStubMessageCall
	karingRequests   []iris.KaringContentListRequest
}

type karingSenderStubMessageCall struct {
	roomID          string
	message         string
	clientRequestID string
}

func (s *karingSenderStubIrisSender) SendMessage(_ context.Context, roomID, message string) error {
	s.sendMessageCalls = append(s.sendMessageCalls, karingSenderStubMessageCall{roomID: roomID, message: message})
	return nil
}

func (s *karingSenderStubIrisSender) SendMessageWithClientRequestID(_ context.Context, roomID, message, clientRequestID string) error {
	s.sendMessageCalls = append(s.sendMessageCalls, karingSenderStubMessageCall{
		roomID: roomID, message: message, clientRequestID: clientRequestID,
	})

	return nil
}

func (s *karingSenderStubIrisSender) SendKaringContentList(_ context.Context, _ string, req *iris.KaringContentListRequest) error {
	s.karingRequests = append(s.karingRequests, *req)
	return nil
}

func TestYouTubeOutboxKaringSenderNilInnerReturnsPinnedError(t *testing.T) {
	sender := YouTubeOutboxKaringSender{sender: nil}

	require.EqualError(t, sender.SendMessage(t.Context(), testAlarmRoomID, "hi"),
		"youtube outbox karing sender: sender is nil")
	require.EqualError(t, sender.SendMessageWithClientRequestID(t.Context(), testAlarmRoomID, "hi", "req-1"),
		"youtube outbox karing sender: sender is nil")
	require.EqualError(t, sender.SendYouTubeOutboxKaring(t.Context(), testAlarmRoomID, &domain.YouTubeOutboxDispatchPayload{}),
		"youtube outbox karing sender: sender is nil")
}

func TestYouTubeOutboxKaringSenderForwardsSendMessage(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := NewYouTubeOutboxKaringSender(stub, nil)

	require.NoError(t, sender.SendMessage(t.Context(), testAlarmRoomID, "hello"))

	require.Len(t, stub.sendMessageCalls, 1)
	assert.Equal(t, testAlarmRoomID, stub.sendMessageCalls[0].roomID)
	assert.Equal(t, "hello", stub.sendMessageCalls[0].message)
	assert.Empty(t, stub.sendMessageCalls[0].clientRequestID)
}

func TestYouTubeOutboxKaringSenderForwardsSendMessageWithClientRequestID(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := NewYouTubeOutboxKaringSender(stub, nil)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), testAlarmRoomID, "hello", "req-1"))

	require.Len(t, stub.sendMessageCalls, 1)
	assert.Equal(t, testAlarmRoomID, stub.sendMessageCalls[0].roomID)
	assert.Equal(t, "hello", stub.sendMessageCalls[0].message)
	assert.Equal(t, "req-1", stub.sendMessageCalls[0].clientRequestID)
}

func TestYouTubeOutboxKaringSenderForwardsKaringChunks(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := NewYouTubeOutboxKaringSender(stub, nil)
	payload := domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindNewVideo,
		AlarmType:  domain.OutboxKindNewVideo.ToAlarmType(),
		ChannelID:  testAlarmChannelID,
		MemberName: "Outbox Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "video000001",
			Payload:   `{"video_id":"video000001","title":"새 영상 제목","thumbnail":[{"url":"https://i.ytimg.com/vi/video000001/maxresdefault.jpg","width":1280,"height":720}]}`,
		}},
	}

	require.NoError(t, sender.SendYouTubeOutboxKaring(t.Context(), "464252100463241", &payload))

	require.Len(t, stub.karingRequests, 1)
	assert.Equal(t, int64(464252100463241), stub.karingRequests[0].ReceiverRoomID)
	require.Len(t, stub.karingRequests[0].Items, 1)
	assert.Equal(t, "새 영상 제목", stub.karingRequests[0].Items[0].Title)
}
