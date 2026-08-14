package sourceobservation

import (
	"testing"
	"time"
)

func TestRelateChannelListAndCoversVideo(t *testing.T) {
	t.Parallel()
	after := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	wide := ChannelListCoverageV1{ChannelID: "UC_A", MaxResults: 10, Exhausted: true}
	narrow := ChannelListCoverageV1{
		ChannelID: "UC_A", MaxResults: 10, Exhausted: true,
		Filters: VideoListFiltersV1{PublishedAfter: &after, PublishedBefore: &before},
	}
	other := ChannelListCoverageV1{ChannelID: "UC_B", MaxResults: 10, Exhausted: true}
	if RelateChannelList(wide, wide) != CoverageEqual {
		t.Fatal("same coverage must be equal")
	}
	if RelateChannelList(narrow, wide) != CoverageCovers {
		t.Fatal("wide evidence must cover a narrower last-positive window")
	}
	if RelateChannelList(wide, narrow) != CoverageCoveredBy {
		t.Fatal("narrow evidence cannot cover a wider last-positive window")
	}
	if RelateChannelList(wide, other) != CoverageDisjoint {
		t.Fatal("different channels are disjoint")
	}
	inside := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	outside := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !ChannelListCoversVideo(narrow, VideoListItemV1{VideoID: "v1", ChannelID: "UC_A", PublishedAt: &inside}) {
		t.Fatal("video inside the time window must be covered")
	}
	if ChannelListCoversVideo(narrow, VideoListItemV1{VideoID: "v1", ChannelID: "UC_A", PublishedAt: &outside}) {
		t.Fatal("video outside the time window must not be covered")
	}
}

func TestRelateShortsList(t *testing.T) {
	t.Parallel()
	complete := ShortsListCoverageV1{ChannelID: "UC_A", MaxResults: 10, Exhausted: true}
	partial := ShortsListCoverageV1{ChannelID: "UC_A", MaxResults: 10, CursorEnd: "c1"}
	if RelateShortsList(complete, complete) != CoverageEqual {
		t.Fatal("exhausted shorts coverage must be equal")
	}
	if RelateShortsList(partial, complete) != CoverageCovers {
		t.Fatal("exhausted shorts evidence must cover a partial candidate")
	}
	if !ShortsListCoversVideo(complete, VideoListItemV1{VideoID: "s1", ChannelID: "UC_A"}) {
		t.Fatal("shorts coverage must include the same channel")
	}
}

func TestAbsenceCapabilityForKind(t *testing.T) {
	t.Parallel()
	if AbsenceCapabilityFor(KindCommunityPage) != AbsencePositiveOnly {
		t.Fatal("community remains POSITIVE_ONLY")
	}
	if AbsenceCapabilityFor(KindVideoList) != AbsenceScoped {
		t.Fatal("video_list is SCOPED_ABSENCE")
	}
	if AbsenceCapabilityFor(KindShortsList) != AbsenceScoped {
		t.Fatal("shorts_list is SCOPED_ABSENCE")
	}
}
