package outbox

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGroupDeliveryRows(t *testing.T) {
	t.Parallel()

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch2", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s3","title":"쇼츠3"}`},
		5: {ID: 5, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
		6: {ID: 6, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"200만"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
		{ID: 104, OutboxID: 4, RoomID: "room1"},
		{ID: 105, OutboxID: 1, RoomID: "room2"},
		{ID: 106, OutboxID: 99, RoomID: "room1"},
		{ID: 107, OutboxID: 5, RoomID: "room1"},
		{ID: 108, OutboxID: 6, RoomID: "room1"},
	}

	groups, orphans := groupDeliveryRows(rows, outboxByID)

	if len(orphans) != 1 || orphans[0].ID != 106 {
		t.Fatalf("orphans = %+v, want [{ID:106}]", orphans)
	}

	if len(groups) != 6 {
		t.Fatalf("group count = %d, want 6", len(groups))
	}

	var shortsGroup *deliveryGroup
	for i := range groups {
		if groups[i].roomID == "room1" && groups[i].channelID == "UCch1" && groups[i].kind == domain.OutboxKindNewShort {
			shortsGroup = &groups[i]
			break
		}
	}
	if shortsGroup == nil {
		t.Fatalf("shorts group for room1+UCch1 not found")
	}
	if len(shortsGroup.rows) != 2 {
		t.Fatalf("shorts group row count = %d, want 2", len(shortsGroup.rows))
	}
	if len(shortsGroup.outboxes) != 2 {
		t.Fatalf("shorts group outbox count = %d, want 2", len(shortsGroup.outboxes))
	}

	milestoneCount := 0
	for _, g := range groups {
		if g.kind == domain.OutboxKindMilestone {
			milestoneCount++
			if len(g.rows) != 1 {
				t.Fatalf("milestone group should be single-item, got %d rows", len(g.rows))
			}
		}
	}
	if milestoneCount != 2 {
		t.Fatalf("milestone group count = %d, want 2", milestoneCount)
	}
}
