package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
)

var errSendRequestNoDeliveries = errors.New("alarm send request has no deliveries")

func (c *Consumer) sendRequestRepository() (SendRequestRepository, error) {
	repository, ok := c.repository.(SendRequestRepository)
	if !ok {
		return nil, errors.New("dispatch repository lacks immutable request support")
	}

	return repository, nil
}

func (c *Consumer) LoadSendRequest(ctx context.Context, envelopes []AlarmQueueEnvelope) (*SendRequest, error) {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return nil, err
	}

	if len(envelopes) == 0 {
		return nil, errSendRequestNoDeliveries
	}

	request, err := repository.LoadSendRequest(ctx, envelopes[0].SendUnitID, claimsFromEnvelopes(envelopes), c.workerID, envelopes[0].ClientRequestID)
	if err != nil {
		return nil, fmt.Errorf("load alarm request: %w", err)
	}

	return request, nil
}

func (c *Consumer) PinSendRequest(ctx context.Context, envelopes []AlarmQueueEnvelope, request SendRequest) (*SendRequest, error) {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return nil, err
	}

	if len(envelopes) == 0 {
		return nil, errSendRequestNoDeliveries
	}

	pinned, err := repository.PinSendRequest(ctx, envelopes[0].SendUnitID, claimsFromEnvelopes(envelopes), c.workerID, envelopes[0].ClientRequestID, request)
	if err != nil {
		return nil, fmt.Errorf("pin alarm request: %w", err)
	}

	return pinned, nil
}

func (c *Consumer) ReissueSendRequest(ctx context.Context, envelopes []AlarmQueueEnvelope, request *SendRequest) error {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return err
	}

	if len(envelopes) == 0 {
		return errSendRequestNoDeliveries
	}

	if request == nil {
		return errors.New("reissue alarm request: pinned request is missing")
	}

	updates, _, _ := failureUpdatesFromEnvelopes(envelopes, nil)

	if err := repository.ReissueSendRequest(ctx, envelopes[0].SendUnitID, c.workerID, request, updates); err != nil {
		return fmt.Errorf("reissue alarm request: %w", err)
	}

	return nil
}
