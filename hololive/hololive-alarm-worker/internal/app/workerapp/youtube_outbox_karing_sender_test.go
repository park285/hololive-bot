package workerapp

import (
	"context"
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type karingSenderStubIrisSender struct {
	sendMessageCalls []karingSenderStubMessageCall
	karingRequests   []iris.KaringContentListRequest
}

type karingSenderStubMessageCall struct {
	roomID  string
	message string
	opts    int
}

func (s *karingSenderStubIrisSender) SendMessage(_ context.Context, roomID, message string, opts ...iris.SendOption) error {
	s.sendMessageCalls = append(s.sendMessageCalls, karingSenderStubMessageCall{roomID: roomID, message: message, opts: len(opts)})
	return nil
}

func (s *karingSenderStubIrisSender) SendKaringContentList(_ context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	s.karingRequests = append(s.karingRequests, *req)
	return nil, nil
}

func TestYouTubeOutboxKaringSenderNilInnerReturnsPinnedError(t *testing.T) {
	sender := youtubeOutboxKaringSender{sender: nil}

	require.EqualError(t, sender.SendMessage(t.Context(), "room-1", "hi"),
		"youtube outbox karing sender: sender is nil")
	require.EqualError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hi", "req-1"),
		"youtube outbox karing sender: sender is nil")
	require.EqualError(t, sender.SendYouTubeOutboxKaring(t.Context(), "room-1", &domain.YouTubeOutboxDispatchPayload{}),
		"youtube outbox karing sender: sender is nil")
}

func TestYouTubeOutboxKaringSenderForwardsSendMessage(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := newYouTubeOutboxKaringSender(egress.NewIrisMessageSender(stub), nil)

	require.NoError(t, sender.SendMessage(t.Context(), "room-1", "hello"))

	require.Len(t, stub.sendMessageCalls, 1)
	assert.Equal(t, "room-1", stub.sendMessageCalls[0].roomID)
	assert.Equal(t, "hello", stub.sendMessageCalls[0].message)
	assert.Equal(t, 0, stub.sendMessageCalls[0].opts)
}

func TestYouTubeOutboxKaringSenderForwardsSendMessageWithClientRequestID(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := newYouTubeOutboxKaringSender(egress.NewIrisMessageSender(stub), nil)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hello", "req-1"))

	require.Len(t, stub.sendMessageCalls, 1)
	assert.Equal(t, "room-1", stub.sendMessageCalls[0].roomID)
	assert.Equal(t, "hello", stub.sendMessageCalls[0].message)
	assert.Equal(t, 1, stub.sendMessageCalls[0].opts)
}

func TestYouTubeOutboxKaringSenderForwardsKaringChunks(t *testing.T) {
	stub := &karingSenderStubIrisSender{}
	sender := newYouTubeOutboxKaringSender(egress.NewIrisMessageSender(stub), nil)
	payload := domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindNewVideo,
		AlarmType:  domain.OutboxKindNewVideo.ToAlarmType(),
		ChannelID:  "UCtest",
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
