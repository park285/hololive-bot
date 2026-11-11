package sourceobservation

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// newPaginatedEnvelope의 관측 시각(slot + 1초)을 기준으로 한 공개 근거 시각들입니다.
var (
	publicationObservedAt = time.Date(2026, time.August, 14, 1, 0, 1, 0, time.UTC)
	publicationCheckedAt  = publicationObservedAt.Add(-time.Second)
	publicationPublished  = publicationCheckedAt.Add(-time.Hour)
	publicationScheduled  = publicationCheckedAt.Add(48 * time.Hour)
)

func publishedListItem(publishedAt, checkedAt time.Time) VideoListItemV1 {
	return VideoListItemV1{
		VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, PublishedAt: new(publishedAt),
		Publication: &VideoPublicationV1{Status: VideoPublicationPublished, PublishedAt: new(publishedAt), CheckedAt: checkedAt},
	}
}

func premiereListItem(scheduledFor, checkedAt time.Time) VideoListItemV1 {
	return VideoListItemV1{
		VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, ScheduledFor: new(scheduledFor), IsPremiere: new(true),
		Publication: &VideoPublicationV1{Status: VideoPublicationUpcomingPremiere, ScheduledFor: new(scheduledFor), CheckedAt: checkedAt},
	}
}

func unresolvedListItem(checkedAt time.Time) VideoListItemV1 {
	return VideoListItemV1{
		VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle,
		Publication: &VideoPublicationV1{Status: VideoPublicationUnresolved, CheckedAt: checkedAt},
	}
}

func videoListGenerationEnvelope(t *testing.T, generation int64, items ...VideoListItemV1) Envelope {
	t.Helper()

	envelope := newPaginatedEnvelope(t, KindVideoList, mustMarshalPayload(t, VideoListV1{
		ChannelID: testChannelID, Videos: items,
		Coverage: ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10},
	}), CompletenessPartial)

	envelope.ContractGeneration = generation

	return envelope
}

func TestVideoListPublicationGenerationAcceptsTrustedEvidence(t *testing.T) {
	t.Parallel()

	tests := map[string]VideoListItemV1{
		"published":                   publishedListItem(publicationPublished, publicationCheckedAt),
		"upcoming premiere":           premiereListItem(publicationScheduled, publicationCheckedAt),
		"unresolved":                  unresolvedListItem(publicationCheckedAt),
		"evidence not collected":      {VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle},
		"checked at observation":      publishedListItem(publicationPublished, publicationObservedAt),
		"published at its check time": publishedListItem(publicationCheckedAt, publicationCheckedAt),
	}

	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := PrepareEnvelope(videoListGenerationEnvelope(t, VideoListPublicationContractGeneration, item)); err != nil {
				t.Fatalf("PrepareEnvelope() error = %v", err)
			}
		})
	}
}

func TestVideoListPublicationGenerationRejectsUntrustedOrInconsistentEvidence(t *testing.T) {
	t.Parallel()

	for _, tests := range []map[string]func() VideoListItemV1{
		itemTimesWithoutEvidenceCases(),
		itemDiffersFromEvidenceCases(),
		invalidEvidenceCases(),
	} {
		for name, build := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				if _, err := PrepareEnvelope(videoListGenerationEnvelope(t, VideoListPublicationContractGeneration, build())); err == nil {
					t.Fatal("PrepareEnvelope() accepted untrusted or inconsistent publication evidence")
				}
			})
		}
	}
}

func itemTimesWithoutEvidenceCases() map[string]func() VideoListItemV1 {
	return map[string]func() VideoListItemV1{
		"lockup published time without evidence": func() VideoListItemV1 {
			return VideoListItemV1{VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, PublishedAt: new(publicationPublished)}
		},
		"lockup schedule without evidence": func() VideoListItemV1 {
			return VideoListItemV1{VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, ScheduledFor: new(publicationScheduled)}
		},
		"premiere flag without evidence": func() VideoListItemV1 {
			return VideoListItemV1{VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, IsPremiere: new(true)}
		},
	}
}

func itemDiffersFromEvidenceCases() map[string]func() VideoListItemV1 {
	return map[string]func() VideoListItemV1{
		"item time differs from evidence": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.PublishedAt = new(publicationPublished.Add(-time.Minute))

			return item
		},
		"published evidence with item schedule": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.ScheduledFor = new(publicationScheduled)

			return item
		},
		"published evidence flagged premiere": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.IsPremiere = new(true)

			return item
		},
		"published evidence with explicit false premiere": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.IsPremiere = new(false)

			return item
		},
		"premiere evidence without premiere flag": func() VideoListItemV1 {
			item := premiereListItem(publicationScheduled, publicationCheckedAt)

			item.IsPremiere = nil

			return item
		},
		"unresolved evidence with item time": func() VideoListItemV1 {
			item := unresolvedListItem(publicationCheckedAt)

			item.PublishedAt = new(publicationPublished)

			return item
		},
	}
}

func invalidEvidenceCases() map[string]func() VideoListItemV1 {
	return map[string]func() VideoListItemV1{
		"unresolved evidence carrying a time": func() VideoListItemV1 {
			item := unresolvedListItem(publicationCheckedAt)

			item.Publication.PublishedAt = new(publicationPublished)
			item.PublishedAt = new(publicationPublished)

			return item
		},
		"published evidence without published time": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.PublishedAt, item.Publication.PublishedAt = nil, nil

			return item
		},
		"published evidence with schedule": func() VideoListItemV1 {
			item := publishedListItem(publicationPublished, publicationCheckedAt)

			item.ScheduledFor, item.Publication.ScheduledFor = new(publicationScheduled), new(publicationScheduled)

			return item
		},
		"premiere evidence with published time": func() VideoListItemV1 {
			item := premiereListItem(publicationScheduled, publicationCheckedAt)

			item.PublishedAt, item.Publication.PublishedAt = new(publicationPublished), new(publicationPublished)

			return item
		},
		"unknown evidence status": func() VideoListItemV1 {
			item := unresolvedListItem(publicationCheckedAt)

			item.Publication.Status = "LOCKUP_RELATIVE"

			return item
		},
		"missing check time": func() VideoListItemV1 {
			return publishedListItem(publicationPublished, time.Time{})
		},
		"checked after observation": func() VideoListItemV1 {
			return publishedListItem(publicationPublished, publicationObservedAt.Add(time.Second))
		},
		"future published time": func() VideoListItemV1 {
			return publishedListItem(publicationCheckedAt.Add(time.Second), publicationCheckedAt)
		},
	}
}

func TestVideoListPublicationGenerationCanonicalizesAndReplays(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	item := publishedListItem(publicationPublished.In(kst), publicationCheckedAt.In(kst))

	prepared, err := PrepareEnvelope(videoListGenerationEnvelope(t, VideoListPublicationContractGeneration, item))
	if err != nil {
		t.Fatalf("PrepareEnvelope() error = %v", err)
	}

	if !strings.Contains(string(prepared.Payload), `"publication"`) || strings.Contains(string(prepared.Payload), "+09:00") {
		t.Fatalf("canonical payload = %s", prepared.Payload)
	}

	requireReplayIdentity(t, prepared)
}

func TestVideoListRejectsUnsupportedGeneration(t *testing.T) {
	t.Parallel()

	for _, generation := range []int64{1, VideoListPublicationContractGeneration + 1} {
		if _, err := PrepareEnvelope(videoListGenerationEnvelope(t, generation)); err == nil {
			t.Fatalf("PrepareEnvelope() accepted unsupported video list generation %d", generation)
		}
	}
}

func TestShortsListRejectsPublication(t *testing.T) {
	t.Parallel()

	shorts := func(item VideoListItemV1) Envelope {
		return newPaginatedEnvelope(t, KindShortsList, mustMarshalPayload(t, ShortsListV1{
			ChannelID: testChannelID, Videos: []VideoListItemV1{item},
			Coverage: ShortsListCoverageV1{ChannelID: testChannelID, MaxResults: 10},
		}), CompletenessPartial)
	}

	plain := VideoListItemV1{VideoID: testVideoID, ChannelID: testChannelID, Title: testTitle, PublishedAt: new(publicationPublished)}
	if _, err := PrepareEnvelope(shorts(plain)); err != nil {
		t.Fatalf("PrepareEnvelope(shorts) error = %v", err)
	}

	if _, err := PrepareEnvelope(shorts(publishedListItem(publicationPublished, publicationCheckedAt))); err == nil {
		t.Fatal("shorts list accepted publication evidence")
	}
}

func requireReplayIdentity(t *testing.T, prepared Envelope) {
	t.Helper()

	replay := prepared

	replay.Payload = bytes.Clone(prepared.Payload)

	again, err := PrepareEnvelope(replay)
	if err != nil {
		t.Fatalf("PrepareEnvelope(replay) error = %v", err)
	}

	if !bytes.Equal(again.Payload, prepared.Payload) || again.ObservationKey != prepared.ObservationKey ||
		again.EvidenceSHA256 != prepared.EvidenceSHA256 {
		t.Fatalf("replay changed identity: payload %s -> %s", prepared.Payload, again.Payload)
	}
}
