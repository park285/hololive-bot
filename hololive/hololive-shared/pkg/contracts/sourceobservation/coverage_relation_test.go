package sourceobservation

import (
	"testing"
	"time"
)

func TestRelateChannelListAndCoversVideo(t *testing.T) {
	t.Parallel()

	after := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	wide := ChannelListCoverageV1{ChannelID: testChannelA, MaxResults: 10, Exhausted: true}
	narrow := ChannelListCoverageV1{
		ChannelID: testChannelA, MaxResults: 10, Exhausted: true,
		Filters: VideoListFiltersV1{PublishedAfter: &after, PublishedBefore: &before},
	}
	other := ChannelListCoverageV1{ChannelID: "UC_B", MaxResults: 10, Exhausted: true}

	if RelateChannelList(&wide, &wide) != CoverageEqual {
		t.Fatal("same coverage must be equal")
	}

	if RelateChannelList(&narrow, &wide) != CoverageCovers {
		t.Fatal("wide evidence must cover a narrower last-positive window")
	}

	if RelateChannelList(&wide, &narrow) != CoverageCoveredBy {
		t.Fatal("narrow evidence cannot cover a wider last-positive window")
	}

	if RelateChannelList(&wide, &other) != CoverageDisjoint {
		t.Fatal("different channels are disjoint")
	}

	inside := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	outside := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	insideVideo := VideoListItemV1{VideoID: "v1", ChannelID: testChannelA, PublishedAt: &inside}

	if !ChannelListCoversVideo(&narrow, &insideVideo) {
		t.Fatal("video inside the time window must be covered")
	}

	outsideVideo := VideoListItemV1{VideoID: "v1", ChannelID: testChannelA, PublishedAt: &outside}
	if ChannelListCoversVideo(&narrow, &outsideVideo) {
		t.Fatal("video outside the time window must not be covered")
	}
}

func TestRelateShortsList(t *testing.T) {
	t.Parallel()

	complete := ShortsListCoverageV1{ChannelID: testChannelA, MaxResults: 10, Exhausted: true}
	partial := ShortsListCoverageV1{ChannelID: testChannelA, MaxResults: 10, CursorEnd: "c1"}

	if RelateShortsList(complete, complete) != CoverageEqual {
		t.Fatal("exhausted shorts coverage must be equal")
	}

	if RelateShortsList(partial, complete) != CoverageCovers {
		t.Fatal("exhausted shorts evidence must cover a partial candidate")
	}

	if !ShortsListCoversVideo(complete, VideoListItemV1{VideoID: "s1", ChannelID: testChannelA}) {
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

	if AbsenceCapabilityFor(KindLiveSnapshot) != AbsenceScoped {
		t.Fatal("live_snapshot is SCOPED_ABSENCE")
	}

	if AbsenceCapabilityFor(KindSchedule) != AbsencePositiveOnly {
		t.Fatal("schedule_snapshot remains POSITIVE_ONLY")
	}
}

func TestLiveCoverageCoversChannel(t *testing.T) {
	t.Parallel()

	coverage := GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelA, "UC_B"}}
	if !LiveCoverageCoversChannel(coverage, testChannelA) {
		t.Fatal("requested channel must be covered")
	}

	if LiveCoverageCoversChannel(coverage, "UC_C") {
		t.Fatal("unrequested channel must not be covered")
	}
}

func TestLiveCoverageCoversSessionRespectsStatusFilter(t *testing.T) {
	t.Parallel()

	liveOnly := GlobalChannelCoverageV1{
		RequestedChannelIDs: []string{testChannelA},
		Filters:             LiveFiltersV1{Statuses: []string{testStatusLive}},
	}
	if !LiveCoverageCoversSession(liveOnly, testChannelA, testStatusLive) {
		t.Fatal("LIVE-only coverage must cover a LIVE session")
	}

	if LiveCoverageCoversSession(liveOnly, testChannelA, "UPCOMING") {
		t.Fatal("LIVE-only coverage must not cover an UPCOMING session")
	}

	empty := GlobalChannelCoverageV1{RequestedChannelIDs: []string{testChannelA}}
	if !LiveCoverageCoversSession(empty, testChannelA, "UPCOMING") {
		t.Fatal("empty status filter covers requested channel sessions")
	}
}
