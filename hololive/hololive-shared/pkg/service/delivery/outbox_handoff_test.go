package delivery

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
)

type handoffTestPublisher struct {
	pending []OutboxItem
	shadow  []OutboxItem
}

func (p *handoffTestPublisher) PublishPending(_ context.Context, items []OutboxItem) error {
	p.pending = append(p.pending, items...)
	return nil
}

func (p *handoffTestPublisher) PublishShadow(_ context.Context, items []OutboxItem) error {
	p.shadow = append(p.shadow, items...)
	return nil
}

func TestOutboxRepositoryCutoverUsesDispatchPublisherWithoutLegacyPool(t *testing.T) {
	t.Parallel()

	publisher := &handoffTestPublisher{}
	repository := NewOutboxRepositoryFromPool(nil, nil, WithDispatchHandoff(handoff.ModeCutover, publisher))

	err := repository.Enqueue(t.Context(), domain.DeliveryKindMemberNewsWeekly, "2026-W32", testRoomID, "주간 뉴스")
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if len(publisher.pending) != 1 || len(publisher.shadow) != 0 {
		t.Fatalf("publisher calls pending=%d shadow=%d", len(publisher.pending), len(publisher.shadow))
	}

	if publisher.pending[0].RoomID != testRoomID || publisher.pending[0].Message != "주간 뉴스" {
		t.Fatalf("pending item = %#v", publisher.pending[0])
	}
}
