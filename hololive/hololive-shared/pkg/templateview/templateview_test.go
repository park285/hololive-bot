package templateview

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildMajorEventViews(t *testing.T) {
	start := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)
	events := []domain.MajorEvent{{
		Title:          "3D Live",
		EventStartDate: &start,
		Members:        []string{"A", "B"},
		Link:           "https://example.com",
	}}

	views := BuildMajorEventViews(events)
	if len(views) != 1 {
		t.Fatalf("unexpected view count: %d", len(views))
	}
	if views[0].Members != "A, B" {
		t.Fatalf("unexpected members: %q", views[0].Members)
	}
}

func TestMemberNewsCategoryLabel(t *testing.T) {
	if got := MemberNewsCategoryLabel(" goods "); got != "굿즈" {
		t.Fatalf("unexpected category label: %q", got)
	}
}
