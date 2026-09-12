package store

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTransitionLogicalGroupRowsExcludeCrossCombinationsBeforeLimit(t *testing.T) {
	for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
		t.Run(mode, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			requested, wanted, crossed := seedGroupQueryCrossCombinations(t, pool)
			transition := newTestTransitionStore(t, pool)

			transition.config.LogicalGroupLimit = 1

			// 이전 독립 ANY 조건에서는 더 오래된 교차 조합 6개가 조회 한도를 모두 차지했습니다.
			require.Len(t, crossed, len(requested)*(transition.config.LogicalGroupLimit+1)+len(requested))

			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

			_, err = tx.Exec(t.Context(), "SELECT set_config('plan_cache_mode', $1, true)", mode)
			require.NoError(t, err)

			rows, err := transition.loadLogicalGroupRows(t.Context(), tx, requested)
			require.NoError(t, err)
			require.Equal(t, wanted, groupQueryRowIDs(rows))
		})
	}
}

func TestTransitionLogicalGroupRowsLocksOnlyRequestedTuples(t *testing.T) {
	pool := dbtest.NewPool(t)
	requested, wanted, crossed := seedGroupQueryCrossCombinations(t, pool)
	transition := newTestTransitionStore(t, pool)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

	_, err = transition.loadLogicalGroupRows(t.Context(), tx, requested)
	require.NoError(t, err)

	probe, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, probe.Rollback(t.Context())) }()

	var lockedID int64

	for _, id := range crossed {
		err = probe.QueryRow(t.Context(), `SELECT id FROM youtube_notification_delivery
			WHERE id = $1 FOR UPDATE NOWAIT`, id).Scan(&lockedID)
		require.NoError(t, err, "unrequested tuple delivery %d must remain unlocked", id)
		require.Equal(t, id, lockedID)
	}

	err = probe.QueryRow(t.Context(), `SELECT id FROM youtube_notification_delivery
		WHERE id = $1 FOR UPDATE NOWAIT`, wanted[0]).Scan(&lockedID)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "requested delivery must retain its row lock: %v", err)
	require.Equal(t, "55P03", pgErr.Code)
}

func TestTransitionLogicalGroupRowsPreservesIdentityCandidatesAndDirectIDs(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	outbox := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewShort, "tuple-short", `{"canonical_post_id":"short:tuple-short"}`)
	delivery := seedGroupQueryDelivery(t, pool, outbox.ID, "room-identity", createdAt)
	requested, err := buildRequestedLogicalGroups([]domain.YouTubeNotificationDelivery{delivery}, map[int64]domain.YouTubeNotificationOutbox{outbox.ID: outbox})
	require.NoError(t, err)

	wanted := make([]int64, 1, 5)

	wanted[0] = delivery.ID

	for _, identity := range []struct {
		contentID string
		payload   string
	}{
		{"short:tuple-short", `{"canonical_post_id":"short:tuple-short"}`},
		{" tuple-short ", `{"canonical_post_id":null}`},
		{"legacy-tuple-short", `{"canonical_post_id":"short:tuple-short"}`},
	} {
		candidate := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewShort, identity.contentID, identity.payload)
		row := seedGroupQueryDelivery(t, pool, candidate.ID, delivery.RoomID, createdAt)

		wanted = append(wanted, row.ID)
	}

	for _, identity := range []struct {
		contentID string
		payload   string
	}{
		{"unrelated-missing", `{}`},
		{"unrelated-null", `{"canonical_post_id":null}`},
	} {
		unrelated := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewShort, identity.contentID, identity.payload)
		seedGroupQueryDelivery(t, pool, unrelated.ID, delivery.RoomID, createdAt)
	}

	directOutbox := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewVideo, "direct-only", `{}`)
	direct := seedGroupQueryDelivery(t, pool, directOutbox.ID, "room-direct", createdAt)

	requested = append(requested, requested[0], requestedLogicalGroup{delivery: direct})
	wanted = append(wanted, direct.ID)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

	rows, err := transition.loadLogicalGroupRows(t.Context(), tx, requested)
	require.NoError(t, err)
	require.Equal(t, wanted, groupQueryRowIDs(rows), "duplicate tuples must not duplicate rows; direct IDs must not require identity candidates")

	_, requestedSet := requestedLogicalKeys(requested[:1])

	_, _, err = transition.claimedResolutionSnapshots(rows[:len(rows)-1], map[int64]struct{}{wanted[2]: {}}, requestedSet)
	require.Error(t, err, "matching malformed payloads must still reach identity validation")
}

func TestTransitionLogicalGroupRowsPreservesScanLimit(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	transition.config.LogicalGroupLimit = 1

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	wanted := make([]int64, 0, 4)

	var requested []requestedLogicalGroup

	for _, contentID := range []string{"tuple-limit", " tuple-limit", "tuple-limit ", " tuple-limit "} {
		outbox := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewVideo, contentID, `{}`)
		delivery := seedGroupQueryDelivery(t, pool, outbox.ID, "room-limit", createdAt)

		wanted = append(wanted, delivery.ID)

		if len(requested) == 0 {
			var err error

			requested, err = buildRequestedLogicalGroups([]domain.YouTubeNotificationDelivery{delivery}, map[int64]domain.YouTubeNotificationOutbox{outbox.ID: outbox})
			require.NoError(t, err)
		}
	}

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

	rows, err := transition.loadLogicalGroupRows(t.Context(), tx, requested)
	require.NoError(t, err)
	require.Equal(t, wanted[:3], groupQueryRowIDs(rows))

	probe, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, probe.Rollback(t.Context())) }()

	var outsideLimit int64

	err = probe.QueryRow(t.Context(), `SELECT id FROM youtube_notification_delivery
		WHERE id = $1 FOR UPDATE NOWAIT`, wanted[3]).Scan(&outsideLimit)
	require.NoError(t, err, "candidate discovery must not lock rows outside the final LIMIT")
	require.Equal(t, wanted[3], outsideLimit)

	_, requestedSet := requestedLogicalKeys(requested)

	_, _, err = transition.claimedResolutionSnapshots(rows, nil, requestedSet)
	require.ErrorContains(t, err, "exceeds limit 1")
}

func seedGroupQueryCrossCombinations(t *testing.T, pool *pgxpool.Pool) ([]requestedLogicalGroup, []int64, []int64) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	requestedDeliveries := make([]domain.YouTubeNotificationDelivery, 0, 2)
	outboxes := make(map[int64]domain.YouTubeNotificationOutbox)
	wanted := make([]int64, 0, 2)
	crossed := make([]int64, 0, 6)

	for kindIndex, kind := range []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindLiveStream} {
		for contentIndex, contentID := range []string{"tuple-x", "tuple-y"} {
			outbox := seedGroupQueryOutbox(t, pool, kind, contentID, `{}`)

			outboxes[outbox.ID] = outbox

			for roomIndex, roomID := range []string{"room-tuple-a", "room-tuple-b"} {
				wantedTuple := kindIndex == contentIndex && contentIndex == roomIndex
				at := createdAt

				if wantedTuple {
					at = at.Add(time.Minute)
				}

				delivery := seedGroupQueryDelivery(t, pool, outbox.ID, roomID, at)

				if wantedTuple {
					requestedDeliveries = append(requestedDeliveries, delivery)
					wanted = append(wanted, delivery.ID)
				} else {
					crossed = append(crossed, delivery.ID)
				}
			}
		}
	}

	requested, err := buildRequestedLogicalGroups(requestedDeliveries, outboxes)
	require.NoError(t, err)

	return requested, wanted, crossed
}

func seedGroupQueryOutbox(t *testing.T, pool *pgxpool.Pool, kind domain.OutboxKind, contentID, payload string) domain.YouTubeNotificationOutbox {
	t.Helper()

	outbox := domain.YouTubeNotificationOutbox{Kind: kind, ContentID: contentID, Payload: payload, ChannelID: "channel-tuple"}
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
		VALUES ($1, $2, $3, $4::jsonb) RETURNING id
	`, kind, outbox.ChannelID, contentID, payload).Scan(&outbox.ID))

	return outbox
}

func seedGroupQueryDelivery(t *testing.T, pool *pgxpool.Pool, outboxID int64, roomID string, createdAt time.Time) domain.YouTubeNotificationDelivery {
	t.Helper()

	delivery := domain.YouTubeNotificationDelivery{OutboxID: outboxID, RoomID: roomID, Status: domain.OutboxStatusPending, CreatedAt: createdAt}
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, outboxID, roomID, delivery.Status, createdAt).Scan(&delivery.ID))

	return delivery
}

func groupQueryRowIDs(rows []transitionRow) []int64 {
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}

	return ids
}
