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

// generation 2는 player 단건 근거가 있는 항목, 근거 없이 시각도 없는 항목, 시각 없는 UNRESOLVED 근거를 받습니다.
// 확인 시각이 관측 시각과 같거나 공개 시각이 확인 시각과 같은 경계도 유효합니다.
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

// generation 2 항목의 시각·Premiere 표시는 근거와 정확히 같아야 하고, 근거는 상태별 시각 규칙과 확인 시각 상한을 지켜야 합니다.
// 목록 문자열에서 만든 시각을 근거 없이 싣거나 미래 공개 시각·관측 뒤의 확인 시각을 신규성 근거로 쓰지 못합니다.
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

// itemTimesWithoutEvidenceCases는 목록 lockup에서 얻은 시각·Premiere 표시를 근거 없이 실은 항목들입니다.
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

// itemDiffersFromEvidenceCases는 근거 자체는 유효하지만 항목 시각·Premiere 표시가 근거와 다른 항목들입니다.
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

// invalidEvidenceCases는 항목과 근거가 같더라도 근거가 상태별 시각 규칙이나 확인 시각 상한을 어기는 항목들입니다.
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

// 근거 시각은 UTC로 정규화되어 canonical payload에 남고, 저장된 payload를 다시 준비해도 같은 identity가 됩니다.
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

// 알 수 없는 video_list 세대는 항목이 없어도 거부합니다.
func TestVideoListRejectsUnsupportedGeneration(t *testing.T) {
	t.Parallel()

	for _, generation := range []int64{1, VideoListPublicationContractGeneration + 1} {
		if _, err := PrepareEnvelope(videoListGenerationEnvelope(t, generation)); err == nil {
			t.Fatalf("PrepareEnvelope() accepted unsupported video list generation %d", generation)
		}
	}
}

// Shorts는 신규 영상 근거의 대상이 아니므로 Publication을 실을 수 없고, 기존 목록 시각은 그대로 받습니다.
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

// requireReplayIdentity는 저장된 canonical payload를 다시 준비해도 payload·관측 identity가 바뀌지 않는지 확인합니다.
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
