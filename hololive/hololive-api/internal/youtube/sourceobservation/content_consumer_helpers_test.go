package sourceobservation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// videoListEnvelope는 각 영상에 기준(2026-08-01) 이후·확인 시각 이전의 player 공개 근거를 붙인 generation 2 목록입니다.
// 신규성 근거 자체를 다루지 않는 부재·title·충돌 테스트용이며, 근거 유무를 다루는 테스트는 videoListItemsEnvelope로 항목을 직접 고릅니다.
func videoListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videoIDs ...string,
) *contract.Envelope {
	t.Helper()

	videos := make([]contract.VideoListItemV1, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		videos = append(videos, trustedVideoItem(videoID, proof.ScheduledFor.Add(-time.Hour), proof.ScheduledFor))
	}

	return videoListItemsEnvelope(t, proof, completeness, videos...)
}

// videoListItemsEnvelope는 항목을 그대로 담은 testChannelID의 generation 2 video_list 관측입니다.
func videoListItemsEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videos ...contract.VideoListItemV1,
) *contract.Envelope {
	t.Helper()

	return channelVideoListEnvelope(t, proof, testChannelID, contract.VideoListPublicationContractGeneration, completeness, videos...)
}

func channelVideoListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	channelID string,
	generation int64,
	completeness contract.Completeness,
	videos ...contract.VideoListItemV1,
) *contract.Envelope {
	t.Helper()

	if videos == nil {
		videos = []contract.VideoListItemV1{}
	}

	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: channelID,
		Videos:    videos,
		Coverage: contract.ChannelListCoverageV1{
			ChannelID: channelID, MaxResults: 10, Exhausted: completeness == contract.CompletenessComplete,
		},
	})
	if err != nil {
		t.Fatalf("marshal video list payload: %v", err)
	}

	return prepareChannelListEnvelope(t, proof, channelID, contract.KindVideoList, generation, completeness, payload)
}

// trustedVideoItem은 player publishDate에서 확인한 공개 영상입니다. Generation 2 항목 시각은 근거와 같아야 합니다.
func trustedVideoItem(videoID string, publishedAt, checkedAt time.Time) contract.VideoListItemV1 {
	return contract.VideoListItemV1{
		VideoID: videoID, ChannelID: testChannelID, Title: videoID, PublishedAt: new(publishedAt),
		Publication: &contract.VideoPublicationV1{
			Status: contract.VideoPublicationPublished, PublishedAt: new(publishedAt), CheckedAt: checkedAt,
		},
	}
}

// premiereVideoItem은 live 콘텐츠가 아닌 대기 상태에서 예정 시각을 확인한 Premiere입니다.
func premiereVideoItem(videoID string, scheduledFor, checkedAt time.Time) contract.VideoListItemV1 {
	return contract.VideoListItemV1{
		VideoID: videoID, ChannelID: testChannelID, Title: videoID, ScheduledFor: new(scheduledFor), IsPremiere: new(true),
		Publication: &contract.VideoPublicationV1{
			Status: contract.VideoPublicationUpcomingPremiere, ScheduledFor: new(scheduledFor), CheckedAt: checkedAt,
		},
	}
}

// unresolvedVideoItem은 이번 관측에 player 근거가 없는(조회하지 않은) 항목입니다.
func unresolvedVideoItem(videoID string) contract.VideoListItemV1 {
	return contract.VideoListItemV1{VideoID: videoID, ChannelID: testChannelID, Title: videoID}
}

func contentClaimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-content-processor",
		LeaseOwner:    testAPILeaseOwner,
		Kinds:         []contract.ObservationKind{contract.KindVideoList, contract.KindShortsList},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func shortsListEnvelope(
	tb testing.TB,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videoIDs ...string,
) *contract.Envelope {
	tb.Helper()

	published := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	videos := make([]contract.VideoListItemV1, 0, len(videoIDs))

	for _, videoID := range videoIDs {
		itemPublished := published

		videos = append(videos, contract.VideoListItemV1{
			VideoID: videoID, ChannelID: testChannelID, Title: videoID, PublishedAt: &itemPublished,
		})
	}

	payload, err := contract.MarshalPayloadV1(contract.ShortsListV1{
		ChannelID: testChannelID,
		Videos:    videos,
		Coverage: contract.ShortsListCoverageV1{
			ChannelID: testChannelID, MaxResults: 10, Exhausted: completeness == contract.CompletenessComplete,
		},
	})
	if err != nil {
		tb.Fatalf("marshal shorts list payload: %v", err)
	}

	return prepareContentListEnvelope(tb, proof, contract.KindShortsList, 1, completeness, payload)
}

func prepareContentListEnvelope(
	tb testing.TB,
	proof *contract.LeaseProof,
	kind contract.ObservationKind,
	generation int64,
	completeness contract.Completeness,
	payload []byte,
) *contract.Envelope {
	tb.Helper()

	return prepareChannelListEnvelope(tb, proof, testChannelID, kind, generation, completeness, payload)
}

func prepareChannelListEnvelope(
	tb testing.TB,
	proof *contract.LeaseProof,
	channelID string,
	kind contract.ObservationKind,
	generation int64,
	completeness contract.Completeness,
	payload []byte,
) *contract.Envelope {
	tb.Helper()

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    kind,
		SubjectKey:         channelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       completeness,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		tb.Fatalf("prepare %s envelope: %v", kind, err)
	}

	return &envelope
}

// moveContentLease는 lease slot을 임의 시각으로 옮겨 이른 slot의 관측이 늦게 도착하는 순서를 만듭니다.
func moveContentLease(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	base *contract.LeaseProof,
	epoch int64,
	at time.Time,
) contract.LeaseProof {
	t.Helper()

	proof := *base

	proof.FenceEpoch = epoch
	proof.ScheduledFor = at

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ScheduledFor); err != nil {
		t.Fatalf("move lease to %s: %v", at, err)
	}

	return proof
}

func seedContentWatermark(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'VIDEO', TRUE, 'old-video')
	`, testChannelID); err != nil {
		t.Fatalf("seed video watermark: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_channel_heads (channel_id, observation_kind, earliest_complete_effective_at)
		VALUES ($1, 'video_list', TIMESTAMPTZ '2026-08-01 00:00:00+00')
	`, testChannelID); err != nil {
		t.Fatalf("seed content channel head: %v", err)
	}
}

func seedShortsWatermark(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'SHORT', TRUE, 'old-short')
	`, testChannelID); err != nil {
		t.Fatalf("seed shorts watermark: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_channel_heads (channel_id, observation_kind, earliest_complete_effective_at)
		VALUES ($1, 'shorts_list', TIMESTAMPTZ '2026-08-01 00:00:00+00')
	`, testChannelID); err != nil {
		t.Fatalf("seed shorts channel head: %v", err)
	}
}

func seedCatalogVideoWithClock(t *testing.T, pool *pgxpool.Pool, videoID string, viewCount int64, lastSeen time.Time) {
	t.Helper()

	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{
		ChannelID: testChannelID, MaxResults: 10, Exhausted: true,
	})
	if err != nil {
		t.Fatalf("marshal coverage: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_videos (
			video_id, channel_id, title, view_count, first_seen_at, last_seen_at
		) VALUES ($1, 'UC_TEST', $1, $2, $3, $3)
	`, videoID, viewCount, lastSeen); err != nil {
		t.Fatalf("seed catalog video: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_content_evidence_clocks (
			video_id, first_positive_effective_at, last_positive_effective_at, last_positive_received_at,
			last_positive_value_sha256, last_positive_scope_sha256, last_positive_coverage
		) VALUES (
			$1, TIMESTAMPTZ '2026-08-14 00:00:00+00', TIMESTAMPTZ '2026-08-14 00:00:00+00',
			TIMESTAMPTZ '2026-08-14 00:00:00+00', $2, $2, $3
		)
	`, videoID, strings.Repeat("ab", 32), coverage); err != nil {
		t.Fatalf("seed content clock: %v", err)
	}
}

func assertContentMissing(t *testing.T, pool *pgxpool.Pool, videoID string, want bool) {
	t.Helper()

	var missing *time.Time

	err := pool.QueryRow(t.Context(), `
		SELECT missing_since_effective_at FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&missing)
	if err != nil {
		t.Fatalf("load missing state for %s: %v", videoID, err)
	}

	if (missing != nil) != want {
		t.Fatalf("video %s missing = %t, want %t", videoID, missing != nil, want)
	}
}

func assertContentWithdrawn(t *testing.T, pool *pgxpool.Pool, videoID string, want bool) {
	t.Helper()

	var withdrawn *time.Time

	err := pool.QueryRow(t.Context(), `
		SELECT withdrawn_at FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&withdrawn)
	if err != nil {
		t.Fatalf("load withdrawn state for %s: %v", videoID, err)
	}

	if (withdrawn != nil) != want {
		t.Fatalf("video %s withdrawn = %t, want %t", videoID, withdrawn != nil, want)
	}
}
