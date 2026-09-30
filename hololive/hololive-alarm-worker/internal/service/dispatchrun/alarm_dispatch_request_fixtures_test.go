package dispatchrun

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

type alarmRequestTestStore struct {
	requests     map[string]*dispatchoutbox.SendRequest
	reissueCalls int
}

func (c *alarmRequestTestStore) LoadSendRequest(_ context.Context, envelopes []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	request := c.requests[envelopes[0].ClientRequestID]
	if request == nil {
		return nil, dispatchoutbox.ErrSendRequestUnpinned
	}

	return request, nil
}

func (c *alarmRequestTestStore) PinSendRequest(_ context.Context, envelopes []domain.AlarmQueueEnvelope, request dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error) {
	if c.requests == nil {
		c.requests = make(map[string]*dispatchoutbox.SendRequest)
	}

	request.ClientRequestID = envelopes[0].ClientRequestID
	request.BaseClientRequestID = request.ClientRequestID
	c.requests[request.ClientRequestID] = &request

	return &request, nil
}

func (c *alarmRequestTestStore) ReissueSendRequest(context.Context, []domain.AlarmQueueEnvelope, string) (bool, error) {
	c.reissueCalls++
	return false, nil
}

func (s *alarmDispatchRunnerTestSender) PrepareMessageRequest(_ context.Context, _, message string) (string, string, error) {
	return message, dispatchoutbox.SendRouteText, nil
}

func (s *alarmDispatchRunnerTestSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) error {
	return s.SendMessageWithClientRequestID(ctx, room, body, id)
}

func (s *alarmDispatchRunnerBlockingSender) PrepareMessageRequest(_ context.Context, _, message string) (string, string, error) {
	return message, dispatchoutbox.SendRouteText, nil
}

func (s *alarmDispatchRunnerBlockingSender) SendPreparedMessage(ctx context.Context, room, body, _, _ string) error {
	return s.SendMessage(ctx, room, body)
}
