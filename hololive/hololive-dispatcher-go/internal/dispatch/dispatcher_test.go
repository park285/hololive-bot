package dispatch

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

func TestDispatcherRunOnce_SendFailureReleasesClaimKeys(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			return []domain.AlarmQueueEnvelope{
				{
					Notification: domain.AlarmNotification{
						RoomID:       "room-1",
						MinutesUntil: 5,
						Channel:      &domain.Channel{ID: "channel-id", Name: "멤버"},
						Stream: &domain.Stream{
							ID:          "stream-1",
							Title:       "테스트 방송",
							ChannelID:   "channel-id",
							ChannelName: "멤버",
						},
					},
					ClaimKeys: []string{"claim-1", "claim-2"},
				},
			}, nil
		},
	}

	fakeSender := &testMessageSender{
		sendMessageFunc: func(ctx context.Context, room, message string, opts ...iris.SendOption) error {
			return errors.New("send failed")
		},
	}

	dispatcher, err := NewDispatcher(
		fakeConsumer,
		fakeSender,
		NewSimpleRenderer(),
		50,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	if runErr := dispatcher.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("RunOnce() error = %v", runErr)
	}

	if len(fakeConsumer.releasedClaimKeys) != 2 {
		t.Fatalf("expected 2 released claim keys, got %d", len(fakeConsumer.releasedClaimKeys))
	}
}

func TestDispatcherRunOnce_DrainError(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			return nil, errors.New("valkey unavailable")
		},
	}
	dispatcher, err := NewDispatcher(
		fakeConsumer,
		&testMessageSender{},
		NewSimpleRenderer(),
		50,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	runErr := dispatcher.RunOnce(context.Background())
	if runErr == nil {
		t.Fatalf("expected error, got nil")
	}
}

type testQueueConsumer struct {
	drainBatchFunc    func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	releaseClaimKeys  func(ctx context.Context, claimKeys []string) error
	releasedClaimKeys []string
}

func (c *testQueueConsumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	if c.drainBatchFunc != nil {
		return c.drainBatchFunc(ctx, maxItems)
	}
	return nil, nil
}

func (c *testQueueConsumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c.releaseClaimKeys != nil {
		return c.releaseClaimKeys(ctx, claimKeys)
	}
	c.releasedClaimKeys = append(c.releasedClaimKeys, claimKeys...)
	return nil
}

type testMessageSender struct {
	sendMessageFunc func(ctx context.Context, room, message string, opts ...iris.SendOption) error
}

func (s *testMessageSender) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	if s.sendMessageFunc != nil {
		return s.sendMessageFunc(ctx, room, message, opts...)
	}
	return nil
}
