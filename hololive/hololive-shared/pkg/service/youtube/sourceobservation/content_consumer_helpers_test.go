package sourceobservation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func videoListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videoIDs ...string,
) *contract.Envelope {
	t.Helper()
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	videos := make([]contract.VideoListItemV1, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		itemPublished := published
		videos = append(videos, contract.VideoListItemV1{
			VideoID: videoID, ChannelID: "UC_TEST", Title: videoID, PublishedAt: &itemPublished,
		})
	}
	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: "UC_TEST",
		Videos:    videos,
		Coverage: contract.ChannelListCoverageV1{
			ChannelID: "UC_TEST", MaxResults: 10, Exhausted: completeness == contract.CompletenessComplete,
		},
	})
	if err != nil {
		t.Fatalf("marshal video list payload: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindVideoList,
		SubjectKey:         "UC_TEST",
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       completeness,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare video list envelope: %v", err)
	}
	return &envelope
}

func contentClaimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-content-processor",
		LeaseOwner:    "api-a",
		Kinds:         []contract.ObservationKind{contract.KindVideoList, contract.KindShortsList},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func shortsListEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	completeness contract.Completeness,
	videoIDs ...string,
) *contract.Envelope {
	t.Helper()
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	videos := make([]contract.VideoListItemV1, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		itemPublished := published
		videos = append(videos, contract.VideoListItemV1{
			VideoID: videoID, ChannelID: "UC_TEST", Title: videoID, PublishedAt: &itemPublished,
		})
	}
	payload, err := contract.MarshalPayloadV1(contract.ShortsListV1{
		ChannelID: "UC_TEST",
		Videos:    videos,
		Coverage: contract.ShortsListCoverageV1{
			ChannelID: "UC_TEST", MaxResults: 10, Exhausted: completeness == contract.CompletenessComplete,
		},
	})
	if err != nil {
		t.Fatalf("marshal shorts list payload: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindShortsList,
		SubjectKey:         "UC_TEST",
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
		t.Fatalf("prepare shorts list envelope: %v", err)
	}
	return &envelope
}

func seedContentWatermark(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'VIDEO', TRUE, 'old-video')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed video watermark: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_content_channel_heads (channel_id, observation_kind, earliest_complete_effective_at)
		VALUES ($1, 'video_list', TIMESTAMPTZ '2026-08-01 00:00:00+00')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed content channel head: %v", err)
	}
}

func seedShortsWatermark(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'SHORT', TRUE, 'old-short')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed shorts watermark: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_content_channel_heads (channel_id, observation_kind, earliest_complete_effective_at)
		VALUES ($1, 'shorts_list', TIMESTAMPTZ '2026-08-01 00:00:00+00')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed shorts channel head: %v", err)
	}
}

func seedCatalogVideoWithClock(t *testing.T, pool *pgxpool.Pool, videoID string, viewCount int64, lastSeen time.Time) {
	t.Helper()
	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{
		ChannelID: "UC_TEST", MaxResults: 10, Exhausted: true,
	})
	if err != nil {
		t.Fatalf("marshal coverage: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_videos (
			video_id, channel_id, title, view_count, first_seen_at, last_seen_at
		) VALUES ($1, 'UC_TEST', $1, $2, $3, $3)
	`, videoID, viewCount, lastSeen); err != nil {
		t.Fatalf("seed catalog video: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
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
	err := pool.QueryRow(context.Background(), `
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
	err := pool.QueryRow(context.Background(), `
		SELECT withdrawn_at FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&withdrawn)
	if err != nil {
		t.Fatalf("load withdrawn state for %s: %v", videoID, err)
	}
	if (withdrawn != nil) != want {
		t.Fatalf("video %s withdrawn = %t, want %t", videoID, withdrawn != nil, want)
	}
}
