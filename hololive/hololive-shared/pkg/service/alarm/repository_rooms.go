package alarm

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// CountAlarmEntries는 관리 목록과 동일하게 host 구독을 방·채널 쌍으로 묶어 센다.
func (r *Repository) CountAlarmEntries(ctx context.Context) (int, error) {
	var count int

	if err := r.pool.QueryRow(ctx, mustSQL("count_alarm_entries.sql")).Scan(&count); err != nil {
		return 0, fmt.Errorf("count alarm entries: %w", err)
	}

	return count, nil
}

func (r *Repository) GetAllDistinctRoomIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, mustSQL("repository_rooms_0009_01.sql"))
	if err != nil {
		return nil, fmt.Errorf("get all distinct room ids: %w", err)
	}
	defer rows.Close()

	var roomIDs []string

	for rows.Next() {
		var roomID string

		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("scan room id: %w", err)
		}

		roomIDs = append(roomIDs, roomID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("room ids rows iteration: %w", err)
	}

	return roomIDs, nil
}

// ListAlarmEntries는 관리 목록용으로 방·채널 쌍을 하나씩 돌려준다. UNIT B 멤버 구독처럼 같은 채널에 host 행이 여럿이어도
// (room_id, channel_id)는 한 번만 나온다. RoomName은 관리자 지정 이름(alarm_room_display_names)을 우선하고, 없으면
// 그 방에서 room_name이 가장 최근에 바뀐(room_name_updated_at) 비어 있지 않은 alarms.room_name(Kakao 방 이름)이며,
// 둘 다 없으면 빈 문자열이다. 방 ID와 같은 room_name은 Kakao 이름을 모를 때 저장된 자리표시자라 대표값에서 뺀다.
// MemberName은 채우지 않는다.
func (r *Repository) ListAlarmEntries(ctx context.Context) ([]*domain.AlarmEntry, error) {
	rows, err := r.pool.Query(ctx, mustSQL("repository_rooms_0036_02.sql"))
	if err != nil {
		return nil, fmt.Errorf("list alarm entries: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AlarmEntry

	for rows.Next() {
		var entry domain.AlarmEntry

		if err := rows.Scan(&entry.RoomID, &entry.ChannelID, &entry.RoomName); err != nil {
			return nil, fmt.Errorf("scan alarm entry: %w", err)
		}

		entries = append(entries, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alarm entries rows iteration: %w", err)
	}

	return entries, nil
}

// SetRoomDisplayName은 관리자 지정 방 이름을 저장한다. 알림 행과 독립이라 알림 재등록·전체 해지 뒤에도 남는다.
func (r *Repository) SetRoomDisplayName(ctx context.Context, roomID, displayName string) error {
	if _, err := r.pool.Exec(ctx, mustSQL("repository_rooms_0062_03.sql"), roomID, displayName); err != nil {
		return fmt.Errorf("upsert room display name: %w", err)
	}

	return nil
}

// DeleteRoomDisplayName은 관리자 지정 방 이름을 지워 관리 목록이 Kakao 방 이름으로 돌아가게 한다.
func (r *Repository) DeleteRoomDisplayName(ctx context.Context, roomID string) error {
	if _, err := r.pool.Exec(ctx, mustSQL("repository_rooms_0074_04.sql"), roomID); err != nil {
		return fmt.Errorf("delete room display name: %w", err)
	}

	return nil
}
