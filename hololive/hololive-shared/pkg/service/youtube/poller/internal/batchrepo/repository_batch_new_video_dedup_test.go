package batchrepo

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func dedupTestOutbox(kind domain.OutboxKind, contentID string) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{Kind: kind, ContentID: contentID}
}

func TestCollectNewVideoContentIDs(t *testing.T) {
	notifications := []*domain.YouTubeNotificationOutbox{
		dedupTestOutbox(domain.OutboxKindNewVideo, "vid1"),
		dedupTestOutbox(domain.OutboxKindNewVideo, "vid1"),
		dedupTestOutbox(domain.OutboxKindNewVideo, "  vid2  "),
		dedupTestOutbox(domain.OutboxKindNewVideo, ""),
		dedupTestOutbox(domain.OutboxKindNewShort, "short1"),
		dedupTestOutbox(domain.OutboxKindCommunityPost, "comm1"),
		nil,
	}

	got := collectNewVideoContentIDs(notifications)
	want := []string{"vid1", "vid2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectNewVideoContentIDs = %v, want %v", got, want)
	}
}

func TestFilterOutKnownNewVideoNotifications(t *testing.T) {
	known := map[string]struct{}{"old1": {}, "old2": {}}
	notifications := []*domain.YouTubeNotificationOutbox{
		dedupTestOutbox(domain.OutboxKindNewVideo, "old1"),
		dedupTestOutbox(domain.OutboxKindNewVideo, "new1"),
		dedupTestOutbox(domain.OutboxKindNewVideo, "  old2 "),
		dedupTestOutbox(domain.OutboxKindNewShort, "old1"),
		dedupTestOutbox(domain.OutboxKindCommunityPost, "old2"),
	}

	got := filterOutKnownNewVideoNotifications(notifications, known)

	wantKind := []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindCommunityPost}
	wantContent := []string{"new1", "old1", "old2"}
	if len(got) != len(wantKind) {
		t.Fatalf("filtered length = %d, want %d: %+v", len(got), len(wantKind), got)
	}
	for i := range got {
		if got[i].Kind != wantKind[i] || got[i].ContentID != wantContent[i] {
			t.Fatalf("got[%d] = {%s,%q}, want {%s,%q}", i, got[i].Kind, got[i].ContentID, wantKind[i], wantContent[i])
		}
	}
}

func TestFilterOutKnownNewVideoNotificationsEmptyKnownKeepsAll(t *testing.T) {
	notifications := []*domain.YouTubeNotificationOutbox{
		dedupTestOutbox(domain.OutboxKindNewVideo, "v1"),
		dedupTestOutbox(domain.OutboxKindNewVideo, "v2"),
	}

	got := filterOutKnownNewVideoNotifications(notifications, map[string]struct{}{})
	if len(got) != 2 {
		t.Fatalf("filtered length = %d, want 2", len(got))
	}
}
