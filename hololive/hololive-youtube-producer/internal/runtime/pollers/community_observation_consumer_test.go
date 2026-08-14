package pollers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestNewCommunityObservationConsumerNilPool(t *testing.T) {
	if got := NewCommunityObservationConsumer(nil, nil, "owner", slog.Default()); got != nil {
		t.Fatal("nil pool must not start a community observation consumer")
	}
}

func TestNewCommunityObservationConsumerWiresNormalizedKeywords(t *testing.T) {
	pool := dbtest.NewPool(t)
	consumer := NewCommunityObservationConsumer(pool, []string{" HoloLive ", "STREAM", "stream"}, "ap-c", slog.Default())
	if consumer == nil || consumer.consumer == nil {
		t.Fatal("expected community observation consumer")
	}
	want := community.NormalizeKeywords([]string{" HoloLive ", "STREAM", "stream"})
	if len(consumer.keywords) != len(want) {
		t.Fatalf("keywords = %#v, want %#v", consumer.keywords, want)
	}
	for i := range want {
		if consumer.keywords[i] != want[i] {
			t.Fatalf("keywords = %#v, want %#v", consumer.keywords, want)
		}
	}
	if consumer.claim.ConsumerName != communityObservationConsumerName {
		t.Fatalf("consumer name = %q", consumer.claim.ConsumerName)
	}
	if consumer.claim.LeaseOwner != "ap-c" {
		t.Fatalf("lease owner = %q", consumer.claim.LeaseOwner)
	}
	if len(consumer.claim.Kinds) != 1 || consumer.claim.Kinds[0] != contract.KindCommunityPage {
		t.Fatalf("claim kinds = %#v", consumer.claim.Kinds)
	}
}

func TestCommunityObservationConsumerTickWithoutPendingRows(t *testing.T) {
	pool := dbtest.NewPool(t)
	consumer := NewCommunityObservationConsumer(pool, nil, "test-owner", slog.Default())
	if err := consumer.tick(context.Background()); err != nil {
		t.Fatalf("empty tick: %v", err)
	}
}

func TestCommunityObservationConsumerTickFailsClosedWhenFenceLoadFails(t *testing.T) {
	pool := dbtest.NewPool(t)
	consumer := NewCommunityObservationConsumer(pool, nil, "test-owner", slog.Default())
	if consumer == nil {
		t.Fatal("expected community observation consumer")
	}
	consumer.repo = sourceobservation.NewRepository(nil)
	consumer.consumer = sourceobservation.NewConsumer(consumer.repo, nil, nil)
	if err := consumer.tick(context.Background()); err == nil {
		t.Fatal("repository failure must fail closed")
	}
}
