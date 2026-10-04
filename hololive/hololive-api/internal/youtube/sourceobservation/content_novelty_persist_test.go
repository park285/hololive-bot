package sourceobservation

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// 기준은 처음 수락한 video_list 관측 시각이며 과거 catalog·0001년 레거시 clock에서 소급하지 않습니다. 더 이른 slot이 늦게
// 처리되면 기준은 더 이른 시각으로만 내려가고, 기준 목록에 있던 영상은 공개 근거가 있어도 알리지 않습니다.
func TestContentNoveltyBaselineTakesEarliestAcceptedListWithoutLegacyBackfill(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, first_seen_at, last_seen_at)
		VALUES ('legacy-row', $1, 'legacy-row', TIMESTAMPTZ '2026-07-01 00:00:00Z', TIMESTAMPTZ '2026-07-01 00:00:00Z'),
		       ('legacy-clock', $1, 'legacy-clock', TIMESTAMPTZ '2026-07-01 00:00:00Z', TIMESTAMPTZ '2026-07-01 00:00:00Z')
	`, testChannelID)
	require.NoError(t, err)

	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_content_evidence_clocks (
			video_id, first_positive_effective_at, last_positive_effective_at, last_positive_received_at,
			last_positive_value_sha256, last_positive_scope_sha256, last_positive_coverage
		) VALUES (
			'legacy-clock', TIMESTAMPTZ '0001-01-01 00:00:00Z', TIMESTAMPTZ '0001-01-01 00:00:00Z',
			TIMESTAMPTZ '2026-07-01 00:00:00Z', $1, $1, $2
		)
	`, strings.Repeat("ab", 32), coverage)
	require.NoError(t, err)

	repo := NewRepository(pool)
	consumer := NewConsumerWithGraces(repo, 0, 0)
	base := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	early, middle, late := base.ScheduledFor, base.ScheduledFor.Add(time.Hour), base.ScheduledFor.Add(2*time.Hour)

	proof := moveContentLease(ctx, t, pool, &base, base.FenceEpoch+1, middle)
	publishVideoItems(t, pool, &proof, contract.CompletenessPartial,
		trustedVideoItem("legacy-row", middle.Add(-30*time.Minute), middle),
		trustedVideoItem("legacy-clock", middle.Add(-30*time.Minute), middle),
		trustedVideoItem("middle-upload", middle.Add(-time.Minute), middle))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertVideoListHead(t, pool, &middle, nil)
	assertNewVideoOutboxes(t, pool)

	proof = moveContentLease(ctx, t, pool, &base, base.FenceEpoch+2, early)
	publishVideoItems(t, pool, &proof, contract.CompletenessPartial, trustedVideoItem("early-upload", early.Add(-time.Minute), early))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertVideoListHead(t, pool, &early, nil)
	assertNewVideoOutboxes(t, pool)

	proof = moveContentLease(ctx, t, pool, &base, base.FenceEpoch+3, late)
	publishVideoItems(t, pool, &proof, contract.CompletenessPartial,
		trustedVideoItem("late-upload", late.Add(-time.Minute), late), unresolvedVideoItem("early-upload"))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertVideoListHead(t, pool, &early, nil)
	assertNewVideoOutboxes(t, pool, "late-upload")
}

// 신뢰 근거가 없는 신규 후보도 canonical·clock은 저장하되 알림 의도는 만들지 않고 novelty_pending으로 남깁니다. Player가 응답했지만
// 시각을 확정하지 못한 관측은 계속 보류하고, 결정적 근거가 오면 정확히 한 번 알립니다. 이후 관측과 replay는 다시 알리지 않습니다.
func TestContentNoveltyPendingCandidateNotifiesOnceWhenTrustedEvidenceArrives(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := t.Context()

	publishVideoItems(t, pool, &proof, contract.CompletenessComplete,
		unresolvedVideoItem("late"),
		trustedVideoItem("old", time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), proof.ScheduledFor))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertTableCount(t, pool, "youtube_videos", 2)
	assertTableCount(t, pool, "youtube_content_evidence_clocks", 2)
	assertNewVideoOutboxes(t, pool)
	require.True(t, contentNoveltyPending(t, pool, "late"), "candidate without evidence must wait")
	require.False(t, contentNoveltyPending(t, pool, "old"), "publication before the baseline must resolve silently")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	unresolved := unresolvedVideoItem("late")

	unresolved.Publication = &contract.VideoPublicationV1{Status: contract.VideoPublicationUnresolved, CheckedAt: proof.ScheduledFor}
	publishVideoItems(t, pool, &proof, contract.CompletenessComplete, unresolved, unresolvedVideoItem("old"))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool)
	require.True(t, contentNoveltyPending(t, pool, "late"), "unresolved player evidence must keep the candidate pending")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	trusted := publishVideoItems(t, pool, &proof, contract.CompletenessComplete,
		trustedVideoItem("late", proof.ScheduledFor.Add(-30*time.Second), proof.ScheduledFor), unresolvedVideoItem("old"))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, "late")
	require.False(t, contentNoveltyPending(t, pool, "late"))

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishVideoItems(t, pool, &proof, contract.CompletenessComplete,
		trustedVideoItem("late", proof.ScheduledFor.Add(-90*time.Second), proof.ScheduledFor), unresolvedVideoItem("old"))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	replay, err := repo.RequestReplay(ctx, ReplayInput{ObservationID: trusted, RequestedBy: testReplayOperator, Reason: "decided novelty must not resend"})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, "late")
}

// 다른 채널이나 Shorts로 이미 저장된 영상은 이 채널 video_list에서 처음 보여도 새 업로드 근거가 아닙니다.
func TestContentNoveltyVideosKnownElsewhereStaySilent(t *testing.T) {
	pool, _, consumer, proof := startContentPersist(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, is_short, first_seen_at, last_seen_at)
		VALUES ('other-channel', 'UC_OTHER', 'other-channel', FALSE, NOW(), NOW()),
		       ('known-short', $1, 'known-short', TRUE, NOW(), NOW())
	`, testChannelID)
	require.NoError(t, err)

	publishedAt := proof.ScheduledFor.Add(-time.Hour)
	publishVideoItems(t, pool, &proof, contract.CompletenessComplete,
		trustedVideoItem("other-channel", publishedAt, proof.ScheduledFor),
		trustedVideoItem("known-short", publishedAt, proof.ScheduledFor),
		trustedVideoItem("fresh", publishedAt, proof.ScheduledFor))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, "fresh")
}

// 같은 채널의 뒤 video_list는 앞선 목록이 끝나기 전에 claim되지 않습니다. 기준을 늦은 목록으로 세우면 그 사이 업로드가
// 기준 목록으로 묻히기 때문입니다. 같은 채널 shorts_list와 다른 채널 목록은 이 순서에 묶이지 않습니다.
func TestContentVideoListClaimsKeepSameChannelOrder(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, member_since_generation
		) VALUES ($1, $2, 'shorts_list', 50, 60000, TRUE, $1)
	`, proof.ProjectionGeneration, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET membership_target_count = 2 WHERE job_key = $1`, proof.JobKey)
	require.NoError(t, err)

	first := publishVideoItems(t, pool, &proof, contract.CompletenessPartial, unresolvedVideoItem("known"))

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	input := publishInput(videoListItemsEnvelope(t, &proof, contract.CompletenessPartial,
		unresolvedVideoItem("known"), trustedVideoItem("new", proof.ScheduledFor.Add(-time.Minute), proof.ScheduledFor)))
	shortsInput := publishInput(shortsListEnvelope(t, &proof, contract.CompletenessPartial, "short"))

	input.Checkpoint.Entries = append(input.Checkpoint.Entries, shortsInput.Checkpoint.Entries...)
	input.Observations = append(input.Observations, shortsInput.Observations...)
	_, err = publishkit.NewPublisher(pool).PublishBatch(ctx, input)
	require.NoError(t, err)

	second := observationIDOf(t, pool, contract.KindVideoList, testChannelID, proof.ScheduledFor)
	shorts := observationIDOf(t, pool, contract.KindShortsList, testChannelID, proof.ScheduledFor)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() + INTERVAL '1 hour' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	batch, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1, "later same-channel video_list must wait for the deferred predecessor")
	require.Equal(t, shorts, batch.Claims[0].ObservationID)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE status = 'CURRENT'`)
	require.NoError(t, err)

	const otherChannel = "UC_OTHER"

	otherProof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, otherChannel, "youtubejs_content")
	otherEnvelope := channelVideoListEnvelope(t, &otherProof, otherChannel, contract.VideoListPublicationContractGeneration,
		contract.CompletenessPartial, contract.VideoListItemV1{VideoID: "other", ChannelID: otherChannel, Title: "other"})
	other, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(otherEnvelope))
	require.NoError(t, err)

	batch, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, other.Results[0].ObservationID, batch.Claims[0].ObservationID)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() - INTERVAL '1 second' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	batch, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, first, batch.Claims[0].ObservationID)
	require.NoError(t, NewConsumerWithGraces(repo, 0, 0).ConsumeClaim(ctx, batch.Claims[0].Claim(batch.ConsumerName)))

	batch, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, second, batch.Claims[0].ObservationID)
}

// 기준 이후 발견한 예정 Premiere는 발견 때 한 번 알리고, 공개 전환 관측과 발견 관측 replay는 다시 알리지 않습니다.
func TestContentNoveltyPremiereNotifiesOnceAndReleaseIsSilent(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := t.Context()
	scheduled := proof.ScheduledFor.Add(2 * time.Hour)

	discovered := publishVideoItems(t, pool, &proof, contract.CompletenessComplete, premiereVideoItem(testVideoID, scheduled, proof.ScheduledFor))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, testVideoID)
	assertLiveSessionPremiere(t, pool, domain.LiveStatusUpcoming, new(true))

	proof = advanceLease(ctx, t, pool, &proof, 3*time.Hour)
	publishVideoItems(t, pool, &proof, contract.CompletenessComplete, trustedVideoItem(testVideoID, scheduled, proof.ScheduledFor))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, testVideoID)

	replay, err := repo.RequestReplay(ctx, ReplayInput{ObservationID: discovered, RequestedBy: testReplayOperator, Reason: "Premiere discovery replay"})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool, testVideoID)
}

// 첫 기준 목록에 있던 예정 Premiere는 live 투영은 하되 알리지 않고, 공개 전환 뒤에도 알리지 않습니다.
func TestContentNoveltyBaselinePremiereStaysSilentThroughRelease(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	consumer := NewConsumerWithGraces(repo, 0, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	scheduled := proof.ScheduledFor.Add(2 * time.Hour)

	publishVideoItems(t, pool, &proof, contract.CompletenessPartial,
		premiereVideoItem(testVideoID, scheduled, proof.ScheduledFor),
		trustedVideoItem("baseline-upload", proof.ScheduledFor.Add(-time.Minute), proof.ScheduledFor))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool)
	assertLiveSessionPremiere(t, pool, domain.LiveStatusUpcoming, new(true))

	proof = advanceLease(ctx, t, pool, &proof, 3*time.Hour)
	publishVideoItems(t, pool, &proof, contract.CompletenessPartial,
		trustedVideoItem(testVideoID, scheduled, proof.ScheduledFor),
		unresolvedVideoItem("baseline-upload"))
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertNewVideoOutboxes(t, pool)
}

// generation 1 video_list는 cutover 전 backlog와 replay를 위해 계속 canonical로 반영되지만 공개 근거가 없으므로 알림을 만들지 않습니다.
// 지원하지 않는 계약으로 dead letter 처리되면 이 backlog가 canonical에서 사라집니다.
func TestContentConsumerLegacyGenerationAppliesCanonicalWithoutNotification(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := t.Context()
	publishedAt := proof.ScheduledFor.Add(-time.Hour)

	setVideoListContractGeneration(t, pool, contract.VideoListLegacyContractGeneration)

	legacy := channelVideoListEnvelope(t, &proof, testChannelID, contract.VideoListLegacyContractGeneration, contract.CompletenessComplete,
		contract.VideoListItemV1{VideoID: testVideoID, ChannelID: testChannelID, Title: "legacy", PublishedAt: &publishedAt})
	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(legacy))
	require.NoError(t, err)
	setVideoListContractGeneration(t, pool, contract.VideoListPublicationContractGeneration)

	observationID := published.Results[0].ObservationID

	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertLegacyCanonical(t, pool, observationID, publishedAt, 0)

	replay, err := repo.RequestReplay(ctx, ReplayInput{ObservationID: observationID, RequestedBy: testReplayOperator, Reason: "legacy generation replay"})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertLegacyCanonical(t, pool, observationID, publishedAt, 1)
}

func assertLegacyCanonical(t *testing.T, pool *pgxpool.Pool, observationID int64, publishedAt time.Time, wantReplays int) {
	t.Helper()

	var (
		status      string
		replayCount int
		stored      *time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, replay_count FROM source_observation_queue WHERE observation_id = $1
	`, observationID).Scan(&status, &replayCount))
	require.Equal(t, string(contract.StatusProcessed), status)
	require.Equal(t, wantReplays, replayCount)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT published_at FROM youtube_videos WHERE video_id = $1`, testVideoID).Scan(&stored))
	require.NotNil(t, stored)
	require.True(t, stored.Equal(publishedAt), "published_at = %s, want %s", stored, publishedAt)
	assertTableCount(t, pool, "youtube_videos", 1)
	assertNewVideoOutboxes(t, pool)
}

func publishVideoItems(
	t *testing.T,
	pool *pgxpool.Pool,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videos ...contract.VideoListItemV1,
) int64 {
	t.Helper()

	published, err := publishkit.NewPublisher(pool).PublishBatch(t.Context(), publishInput(videoListItemsEnvelope(t, proof, completeness, videos...)))
	require.NoError(t, err)
	require.Len(t, published.Results, 1)

	return published.Results[0].ObservationID
}

func setVideoListContractGeneration(t *testing.T, pool *pgxpool.Pool, generation int64) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE observation_contract_generations
		SET current_generation = $1
		WHERE provider = 'youtubejs' AND observation_kind = 'video_list'
	`, generation)
	require.NoError(t, err)
}

func observationIDOf(t *testing.T, pool *pgxpool.Pool, kind contract.ObservationKind, subjectKey string, scheduledFor time.Time) int64 {
	t.Helper()

	var id int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT id FROM source_observations
		WHERE observation_kind = $1 AND subject_key = $2 AND scheduled_for = $3
	`, kind, subjectKey, scheduledFor).Scan(&id))

	return id
}

func contentNoveltyPending(t *testing.T, pool *pgxpool.Pool, videoID string) bool {
	t.Helper()

	var pending bool

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT novelty_pending FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&pending))

	return pending
}

func assertVideoListHead(t *testing.T, pool *pgxpool.Pool, wantBaseline, wantComplete *time.Time) {
	t.Helper()

	var baseline, complete *time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT earliest_baseline_effective_at, earliest_complete_effective_at
		FROM youtube_content_channel_heads
		WHERE channel_id = $1 AND observation_kind = 'video_list'
	`, testChannelID).Scan(&baseline, &complete))
	requireSameOptionalTime(t, "earliest_baseline_effective_at", wantBaseline, baseline)
	requireSameOptionalTime(t, "earliest_complete_effective_at", wantComplete, complete)
}

func requireSameOptionalTime(t *testing.T, name string, want, got *time.Time) {
	t.Helper()

	if want == nil || got == nil {
		require.Equal(t, want == nil, got == nil, "%s = %v, want %v", name, got, want)

		return
	}

	require.True(t, got.Equal(*want), "%s = %s, want %s", name, got, want)
}

func assertNewVideoOutboxes(t *testing.T, pool *pgxpool.Pool, videoIDs ...string) {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT content_id FROM youtube_notification_outbox WHERE kind = $1 ORDER BY content_id
	`, domain.OutboxKindNewVideo)
	require.NoError(t, err)

	defer rows.Close()

	got := []string{}

	for rows.Next() {
		var id string

		require.NoError(t, rows.Scan(&id))

		got = append(got, id)
	}

	require.NoError(t, rows.Err())

	if videoIDs == nil {
		videoIDs = []string{}
	}

	require.Equal(t, videoIDs, got)
}
