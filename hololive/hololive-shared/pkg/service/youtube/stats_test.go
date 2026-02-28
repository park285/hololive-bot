package youtube

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDetermineMemberName(t *testing.T) {
	svc := &StatsService{}
	lookup := map[string]string{"ch1": "lookup"}
	prev := &domain.TimestampedStats{MemberName: "prev"}

	if got := svc.determineMemberName("ch1", prev, lookup); got != "lookup" {
		t.Fatalf("expected lookup name, got %s", got)
	}
	if got := svc.determineMemberName("ch2", prev, lookup); got != "prev" {
		t.Fatalf("expected prev name, got %s", got)
	}
	if got := svc.determineMemberName("ch2", nil, nil); got != "" {
		t.Fatalf("expected empty name, got %s", got)
	}
}

func TestSaveCurrentStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &StatsService{logger: logger}

	item := &youtube.Channel{
		Id: "ch1",
		Statistics: &youtube.ChannelStatistics{
			SubscriberCount: 10,
			VideoCount:      2,
			ViewCount:       3,
		},
	}
	prev := &domain.TimestampedStats{SubscriberCount: 7}

	change := svc.saveCurrentStats(context.Background(), item, "name", prev)
	if change != 3 {
		t.Fatalf("expected subscriber change 3, got %d", change)
	}
}
