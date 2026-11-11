package alarmdispatch

import (
	"context"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type alarmRequestTestStore struct {
	requests     map[string]*dispatchoutbox.SendRequest
	reissueCalls int
}

func (c *alarmRequestTestStore) LoadSendRequest(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	request := c.requests[envelopes[0].ClientRequestID]
	if request == nil {
		return nil, dispatchoutbox.ErrSendRequestUnpinned
	}

	return request, nil
}

func (c *alarmRequestTestStore) PinSendRequest(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, request dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error) {
	if c.requests == nil {
		c.requests = make(map[string]*dispatchoutbox.SendRequest)
	}

	request.ClientRequestID = envelopes[0].ClientRequestID
	request.BaseClientRequestID = request.ClientRequestID
	c.requests[request.ClientRequestID] = &request

	return &request, nil
}

// ReissueSendRequest는 재발급 세대가 남지 않은 저장소처럼 동작해 실패 경로가 격리로 끝나게 한다.
func (c *alarmRequestTestStore) ReissueSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope, *dispatchoutbox.SendRequest) error {
	c.reissueCalls++
	return sendfence.ErrGenerationExhausted
}

func (s *alarmDispatchRunnerTestSender) PrepareMessageRequest(_ context.Context, _ string, message delivery.MessageVariants) (string, string, error) {
	return message.Plain, dispatchoutbox.SendRouteText, nil
}

func (s *alarmDispatchRunnerTestSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return alarmTestAdmission(id, s.SendMessageWithClientRequestID(ctx, room, body, id))
}

func (s *alarmDispatchRunnerBlockingSender) PrepareMessageRequest(_ context.Context, _ string, message delivery.MessageVariants) (string, string, error) {
	return message.Plain, dispatchoutbox.SendRouteText, nil
}

func (s *alarmDispatchRunnerBlockingSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return alarmTestAdmission(id, s.SendMessage(ctx, room, body))
}

func alarmTestAdmission(clientRequestID string, err error) (string, error) {
	if err != nil {
		return "", err
	}

	return "iris:" + clientRequestID, nil
}

// 내용 계약은 방 유형과 무관하게 두 변형 모두 지켜야 한다.
func messageTexts(message delivery.MessageVariants) []string {
	return []string{message.Plain, message.RichText}
}
