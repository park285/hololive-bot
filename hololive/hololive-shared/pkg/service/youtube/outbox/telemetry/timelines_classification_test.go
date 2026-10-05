package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

type storedClassification struct {
	status             sql.NullString
	delaySource        sql.NullString
	internalDelayCause sql.NullString
}

func TestNewRepositoryRejectsNilQuerier(t *testing.T) {
	var typedNil *pgxpool.Pool

	for name, db := range map[string]dbx.Querier{"untyped nil": nil, "typed nil pool": typedNil} {
		t.Run(name, func(t *testing.T) {
			repository, err := NewRepository(db)
			require.ErrorIs(t, err, ErrNilQuerier)
			require.Nil(t, repository)
		})
	}
}

func seedClassificationTracking(
	ctx context.Context,
	t *testing.T,
	db dbx.Querier,
	kind domain.OutboxKind,
	contentID, canonicalContentID string,
	detectedAt time.Time,
) {
	t.Helper()

	sentAt := detectedAt.Add(30 * time.Second)

	_, err := db.Exec(ctx, `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at)
		VALUES ($1, $2, $3, 'UC_classification', $4, $4, $5)
	`, string(kind), contentID, canonicalContentID, detectedAt, sentAt)
	require.NoError(t, err)
}

func loadStoredClassifications(
	ctx context.Context,
	t *testing.T,
	db dbx.Querier,
	kind domain.OutboxKind,
	contentID string,
) []storedClassification {
	t.Helper()

	rows, err := db.Query(ctx, `
		SELECT latency_classification_status, delay_source, internal_delay_cause
		FROM youtube_content_alarm_tracking
		WHERE kind = $1 AND content_id = $2
		ORDER BY canonical_content_id
	`, string(kind), contentID)
	require.NoError(t, err)

	defer rows.Close()

	var out []storedClassification

	for rows.Next() {
		var stored storedClassification

		require.NoError(t, rows.Scan(&stored.status, &stored.delaySource, &stored.internalDelayCause))

		out = append(out, stored)
	}

	require.NoError(t, rows.Err())

	return out
}

// expectedClassificationFor는 timeline 조회가 identity의 첫 행에 매긴 분류를 저장 형태로 돌려준다.
func expectedClassificationFor(
	t *testing.T,
	rows []timeline.PostDeliveryTimeline,
	kind domain.OutboxKind,
	contentID string,
) storedClassification {
	t.Helper()

	for i := range rows {
		if rows[i].OutboxKind != kind || rows[i].ContentID != contentID {
			continue
		}

		status, delaySource, internalDelayCause := normalizedPostLatencyClassificationPersistenceValues(&rows[i])

		return storedClassification{
			status:             sql.NullString{String: string(status), Valid: true},
			delaySource:        sql.NullString{String: string(delaySource), Valid: true},
			internalDelayCause: sql.NullString{String: string(internalDelayCause), Valid: true},
		}
	}

	t.Fatalf("timeline row for %s/%s not found", kind, contentID)

	return storedClassification{}
}

func TestPersistPostLatencyClassificationsWritesAllIdentitiesInOneStatement(t *testing.T) {
	ctx := t.Context()
	counting := &execCountingQuerier{inner: dbtest.NewPool(t)}
	repository := mustNewTestRepository(t, counting)
	base := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	const identityCount = 40

	identities := make([]timeline.PostTrackingIdentity, 0, identityCount)
	for i := range identityCount {
		kind := domain.OutboxKindCommunityPost

		if i%2 == 1 {
			kind = domain.OutboxKindNewShort
		}

		contentID := fmt.Sprintf("batch-%02d", i)
		seedClassificationTracking(ctx, t, counting.inner, kind, contentID, contentID, base.Add(time.Duration(i)*time.Minute))

		identities = append(identities, timeline.PostTrackingIdentity{Kind: kind, ContentID: contentID})
	}

	seedClassificationTracking(ctx, t, counting.inner, domain.OutboxKindCommunityPost, "untouched", "untouched", base)

	timelines, err := repository.ListPostDeliveryTimelinesByTrackingIdentities(ctx, identities)
	require.NoError(t, err)
	require.Len(t, timelines, identityCount)

	counting.execCalls = 0

	// 중복 identity는 한 번만 갱신 대상이 된다.
	require.NoError(t, repository.PersistPostLatencyClassificationsByIdentities(ctx, append(identities, identities[0], identities[1])))
	require.Equal(t, 1, counting.execCalls, "classification writes must be one batched statement, not one Exec per identity")

	for _, identity := range identities {
		stored := loadStoredClassifications(ctx, t, counting.inner, identity.Kind, identity.ContentID)
		require.Len(t, stored, 1)
		require.Equal(t, expectedClassificationFor(t, timelines, identity.Kind, identity.ContentID), stored[0], "%s/%s", identity.Kind, identity.ContentID)
	}

	untouched := loadStoredClassifications(ctx, t, counting.inner, domain.OutboxKindCommunityPost, "untouched")
	require.Len(t, untouched, 1)
	require.Equal(t, storedClassification{}, untouched[0])
}

func TestTrackingIdentityQueriesMatchKindContentPairsOnly(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := mustNewTestRepository(t, pool)
	base := time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)

	// 요청 쌍 (COMMUNITY_POST, pair-a), (NEW_SHORT, pair-b)의 교차 조합도 tracking에 존재한다.
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindCommunityPost, "pair-a", "pair-a", base)
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindNewShort, "pair-b", "pair-b", base)
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindNewShort, "pair-a", "pair-a", base)
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindCommunityPost, "pair-b", "pair-b", base)

	identities := []timeline.PostTrackingIdentity{
		{Kind: domain.OutboxKindCommunityPost, ContentID: "pair-a"},
		{Kind: domain.OutboxKindNewShort, ContentID: "pair-b"},
	}

	timelines, err := repository.ListPostDeliveryTimelinesByTrackingIdentities(ctx, identities)
	require.NoError(t, err)

	got := make(map[string]struct{}, len(timelines))
	for i := range timelines {
		got[timeline.PostTrackingIdentityKey(timelines[i].OutboxKind, timelines[i].ContentID)] = struct{}{}
	}

	require.Equal(t, map[string]struct{}{
		timeline.PostTrackingIdentityKey(domain.OutboxKindCommunityPost, "pair-a"): {},
		timeline.PostTrackingIdentityKey(domain.OutboxKindNewShort, "pair-b"):      {},
	}, got)

	require.NoError(t, repository.PersistPostLatencyClassificationsByIdentities(ctx, identities))

	for _, identity := range identities {
		stored := loadStoredClassifications(ctx, t, pool, identity.Kind, identity.ContentID)
		require.Len(t, stored, 1)
		require.True(t, stored[0].status.Valid, "%s/%s must be classified", identity.Kind, identity.ContentID)
	}

	for _, crossed := range []timeline.PostTrackingIdentity{
		{Kind: domain.OutboxKindNewShort, ContentID: "pair-a"},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "pair-b"},
	} {
		stored := loadStoredClassifications(ctx, t, pool, crossed.Kind, crossed.ContentID)
		require.Len(t, stored, 1)
		require.Equal(t, storedClassification{}, stored[0], "%s/%s is not a requested pair", crossed.Kind, crossed.ContentID)
	}
}

func TestPersistPostLatencyClassificationsAppliesFirstTimelineRowToSharedIdentity(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := mustNewTestRepository(t, pool)
	base := time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC)

	// 같은 (kind, content_id)를 가진 tracking 행 둘은 timeline에서 별도 행이지만 같은 분류로 갱신된다.
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindCommunityPost, "shared", "shared-canonical-1", base)
	seedClassificationTracking(ctx, t, pool, domain.OutboxKindCommunityPost, "shared", "shared-canonical-2", base.Add(-time.Hour))

	identities := []timeline.PostTrackingIdentity{{Kind: domain.OutboxKindCommunityPost, ContentID: "shared"}}

	timelines, err := repository.ListPostDeliveryTimelinesByTrackingIdentities(ctx, identities)
	require.NoError(t, err)
	require.Len(t, timelines, 2)

	require.NoError(t, repository.PersistPostLatencyClassificationsByIdentities(ctx, identities))

	want := expectedClassificationFor(t, timelines, domain.OutboxKindCommunityPost, "shared")
	require.Equal(t, []storedClassification{want, want}, loadStoredClassifications(ctx, t, pool, domain.OutboxKindCommunityPost, "shared"))
}

func TestPersistPostLatencyClassificationsInTransactionSeesCurrentAttemptAndRollsBack(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	seedClassificationTracking(ctx, t, pool, domain.OutboxKindCommunityPost, "tx-post", "tx-post", base)

	var outboxID int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
		VALUES ('COMMUNITY_POST', 'UC_classification', 'tx-post', '{}'::jsonb)
		RETURNING id
	`).Scan(&outboxID))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			require.NoError(t, rollbackErr)
		}
	}()

	repository := mustNewTestRepository(t, tx)

	_, err = tx.Exec(ctx, `
		INSERT INTO youtube_notification_delivery_telemetry
			(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id,
			 alarm_type, dedupe_key, delivery_mode, send_result, event_at)
		VALUES (9001, 1, $1, 'UC_classification', 'tx-post', 'tx-post', 'room-tx', $2, 'dedupe-tx-post', 'grouped', 'success', $3)
	`, outboxID, string(domain.AlarmTypeCommunity), base.Add(time.Minute))
	require.NoError(t, err)

	identities := []timeline.PostTrackingIdentity{{Kind: domain.OutboxKindCommunityPost, ContentID: "tx-post"}}

	timelines, err := repository.ListPostDeliveryTimelinesByTrackingIdentities(ctx, identities)
	require.NoError(t, err)
	require.Len(t, timelines, 1)
	require.Equal(t, int64(1), timelines[0].SuccessSendCount, "uncommitted attempt in the same transaction must be visible")

	require.NoError(t, repository.PersistPostLatencyClassificationsByIdentities(ctx, identities))
	require.Equal(t,
		[]storedClassification{expectedClassificationFor(t, timelines, domain.OutboxKindCommunityPost, "tx-post")},
		loadStoredClassifications(ctx, t, tx, domain.OutboxKindCommunityPost, "tx-post"),
	)

	require.NoError(t, tx.Rollback(ctx))

	require.Equal(t,
		[]storedClassification{{}},
		loadStoredClassifications(ctx, t, pool, domain.OutboxKindCommunityPost, "tx-post"),
		"rolled back classification must not be visible",
	)
}
