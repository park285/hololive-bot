package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Consumer) sendRequestRepository() (SendRequestRepository, error) {
	repository, ok := c.repository.(SendRequestRepository)
	if !ok {
		return nil, errors.New("dispatch repository lacks immutable request support")
	}

	return repository, nil
}

// LoadSendRequest는 소유한 전체 send unit의 고정 요청을 읽고 과거 미고정 발송을 거절한다.
func (c *Consumer) LoadSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (*SendRequest, error) {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return nil, err
	}

	if len(envelopes) == 0 {
		return nil, ErrSendRequestFence
	}

	request, err := repository.LoadSendRequest(ctx, envelopes[0].SendUnitID, idsFromEnvelopes(envelopes), c.workerID)
	if err != nil {
		return nil, fmt.Errorf("load alarm request: %w", err)
	}

	return request, nil
}

// PinSendRequest는 첫 외부 발송 전 본문·경로·membership을 고정한다.
func (c *Consumer) PinSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, request SendRequest) (*SendRequest, error) {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return nil, err
	}

	if len(envelopes) == 0 {
		return nil, ErrSendRequestFence
	}

	pinned, err := repository.PinSendRequest(ctx, envelopes[0].SendUnitID, idsFromEnvelopes(envelopes), c.workerID, request)
	if err != nil {
		return nil, fmt.Errorf("pin alarm request: %w", err)
	}

	return pinned, nil
}

// ReissueSendRequest는 retry 예산이 남은 전체 그룹의 generation과 상태를 함께 확정한다.
func (c *Consumer) ReissueSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, expectedID string) (bool, error) {
	repository, err := c.sendRequestRepository()
	if err != nil {
		return false, err
	}

	if len(envelopes) == 0 {
		return false, ErrSendRequestFence
	}

	updates, _, _ := failureUpdatesFromEnvelopes(envelopes, nil)

	reissued, err := repository.ReissueSendRequest(ctx, envelopes[0].SendUnitID, idsFromEnvelopes(envelopes), c.workerID, expectedID, updates)
	if err != nil {
		return false, fmt.Errorf("reissue alarm request: %w", err)
	}

	return reissued, nil
}
