package alarm

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

type subscriberSnapshotDB struct {
	*alarmTargetLookupTestDB

	afterRead func()
}

func (db *subscriberSnapshotDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := db.alarmTargetLookupTestDB.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query subscriber snapshot: %w", err)
	}

	return &subscriberSnapshotRows{Rows: rows, afterRead: db.afterRead}, nil
}

type subscriberSnapshotRows struct {
	pgx.Rows

	afterRead func()
}

func (rows *subscriberSnapshotRows) Close() {
	rows.Rows.Close()

	if rows.afterRead != nil {
		action := rows.afterRead

		rows.afterRead = nil

		action()
	}
}

func TestSubscriberReadThroughPreservesConcurrentMutation(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, add := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch=%v/add=%v", batch, add), func(t *testing.T) {
				ctx := t.Context()
				db := newAlarmTargetLookupTestDB(t)
				client := testutil.NewTestCacheService(ctx, t)
				channel := "UC_snapshot_consistency"
				room := "snapshot-room"
				key := sharedalarmkeys.BuildChannelSubscriberKey(channel, domain.AlarmTypeLive)
				emptyKey := sharedalarmkeys.BuildChannelSubscriberEmptyKey(channel, domain.AlarmTypeLive)
				record := &domain.Alarm{RoomID: room, ChannelID: channel, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}}

				if !add {
					requireAlarmRecord(t, db, record)
				}

				snapshot := &subscriberSnapshotDB{alarmTargetLookupTestDB: db}

				snapshot.afterRead = func() {
					if add {
						_, err := db.Exec(ctx, `INSERT INTO alarms (room_id,user_id,channel_id,alarm_types) VALUES ($1,'',$2,ARRAY['LIVE']::alarm_type[])`, room, channel)
						require.NoError(t, err)

						_, err = client.SAdd(ctx, key, []string{room})
						require.NoError(t, err)
						require.NoError(t, client.Del(ctx, emptyKey))
					} else {
						_, err := db.Exec(ctx, `DELETE FROM alarms WHERE channel_id=$1 AND room_id=$2`, channel, room)
						require.NoError(t, err)

						_, err = client.SRem(ctx, key, []string{room})
						require.NoError(t, err)
					}
				}

				if batch {
					_, err := ResolveUncachedChannelSubscribersByType(ctx, client, snapshot, []string{channel}, domain.AlarmTypeLive)
					require.NoError(t, err)
				} else {
					_, err := ResolveChannelSubscribersByType(ctx, client, snapshot, channel, domain.AlarmTypeLive)
					require.NoError(t, err)
				}

				// 구독 추가 후 set eviction이 있어도 오래된 empty marker가 DB 확인을 막으면 안 된다.
				if add {
					require.NoError(t, client.Del(ctx, key))
				}

				next, err := ResolveChannelSubscribersByType(ctx, client, db, channel, domain.AlarmTypeLive)
				require.NoError(t, err)

				if add {
					require.Equal(t, []string{room}, next)
				} else {
					require.Empty(t, next)
				}
			})
		}
	}
}
