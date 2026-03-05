package alarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// Repository: 알람 데이터의 영속 저장소 (PostgreSQL)
type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewRepository: 새로운 알람 Repository를 생성합니다.
func NewRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}

// Add: 알람을 DB에 추가한다. 이미 존재하면 무시한다. (upsert)
// room_id + channel_id 기준 unique (방 기반 시스템)
func (r *Repository) Add(ctx context.Context, alarm *domain.Alarm) error {
	alarmTypes := alarm.AlarmTypes
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	query := `
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (room_id, channel_id) DO UPDATE
		SET member_name = COALESCE(EXCLUDED.member_name, alarms.member_name),
		    room_name = COALESCE(EXCLUDED.room_name, alarms.room_name),
		    user_name = COALESCE(EXCLUDED.user_name, alarms.user_name),
		    user_id = EXCLUDED.user_id,
		    alarm_types = EXCLUDED.alarm_types
	`

	typesValue, _ := alarmTypes.Value()
	_, err := r.pool.Exec(ctx, query,
		alarm.RoomID, alarm.UserID, alarm.ChannelID,
		alarm.MemberName, alarm.RoomName, alarm.UserName,
		typesValue,
	)
	if err != nil {
		return fmt.Errorf("add alarm: %w", err)
	}
	return nil
}

// Remove: 특정 알람을 DB에서 삭제합니다. (방 기반: room_id + channel_id)
func (r *Repository) Remove(ctx context.Context, roomID, channelID string) error {
	query := `DELETE FROM alarms WHERE room_id = $1 AND channel_id = $2`
	_, err := r.pool.Exec(ctx, query, roomID, channelID)
	if err != nil {
		return fmt.Errorf("remove alarm: %w", err)
	}
	return nil
}

// ClearByRoom: 특정 방의 모든 알람을 삭제합니다.
func (r *Repository) ClearByRoom(ctx context.Context, roomID string) (int64, error) {
	query := `DELETE FROM alarms WHERE room_id = $1`
	cmdTag, err := r.pool.Exec(ctx, query, roomID)
	if err != nil {
		return 0, fmt.Errorf("clear alarms: %w", err)
	}
	return cmdTag.RowsAffected(), nil
}

// FindByRoom: 특정 방의 모든 알람을 조회합니다. (방 기반 PRIMARY)
func (r *Repository) FindByRoom(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	query := `
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at
		FROM alarms
		WHERE room_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("find alarms by room: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

// FindByChannel: 특정 채널의 모든 구독자 알람을 조회합니다.
func (r *Repository) FindByChannel(ctx context.Context, channelID string) ([]*domain.Alarm, error) {
	query := `
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at
		FROM alarms
		WHERE channel_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

// FindByChannelAndType: 특정 채널 + 알람 타입의 구독자 알람을 조회합니다.
func (r *Repository) FindByChannelAndType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]*domain.Alarm, error) {
	query := `
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at
		FROM alarms
		WHERE channel_id = $1 AND $2 = ANY(alarm_types)
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, channelID, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel and type: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

// GetMemberName: 채널ID에 해당하는 멤버 이름을 조회합니다.
// 해당 채널에 알람을 설정한 적이 있는 레코드에서 member_name을 가져온다.
func (r *Repository) GetMemberName(ctx context.Context, channelID string) (string, error) {
	query := `
		SELECT member_name FROM alarms
		WHERE channel_id = $1 AND member_name IS NOT NULL AND member_name != ''
		ORDER BY created_at DESC
		LIMIT 1
	`

	var memberName string
	err := r.pool.QueryRow(ctx, query, channelID).Scan(&memberName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}
	return memberName, nil
}

// LoadAll: 모든 알람을 조회한다. (앱 시작 시 캐시 워밍용)
func (r *Repository) LoadAll(ctx context.Context) ([]*domain.Alarm, error) {
	query := `
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at
		FROM alarms
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load all alarms: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

// GetAllChannelIDs: 알람이 설정된 모든 채널 ID를 조회합니다.
func (r *Repository) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	query := `SELECT DISTINCT channel_id FROM alarms`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all channel ids: %w", err)
	}
	defer rows.Close()

	channelIDs := make([]string, 0, 64)
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("scan channel id: %w", err)
		}
		channelIDs = append(channelIDs, channelID)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate channel ids: %w", rowsErr)
	}
	return channelIDs, nil
}

// GetAllMemberNames: 모든 채널ID → 멤버이름 매핑을 조회합니다.
func (r *Repository) GetAllMemberNames(ctx context.Context) (map[string]string, error) {
	query := `
		SELECT DISTINCT ON (channel_id) channel_id, member_name
		FROM alarms
		WHERE member_name IS NOT NULL AND member_name != ''
		ORDER BY channel_id, created_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all member names: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var channelID, memberName string
		if err := rows.Scan(&channelID, &memberName); err != nil {
			return nil, fmt.Errorf("scan member name: %w", err)
		}
		result[channelID] = memberName
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate member names: %w", rowsErr)
	}
	return result, nil
}

func (r *Repository) scanAlarms(rows pgx.Rows) ([]*domain.Alarm, error) {
	var alarms []*domain.Alarm
	for rows.Next() {
		var a domain.Alarm
		var memberName, roomName, userName *string
		var alarmTypesStr *string

		err := rows.Scan(
			&a.ID, &a.RoomID, &a.UserID, &a.ChannelID,
			&memberName, &roomName, &userName, &alarmTypesStr, &a.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan alarm: %w", err)
		}

		if memberName != nil {
			a.MemberName = *memberName
		}
		if roomName != nil {
			a.RoomName = *roomName
		}
		if userName != nil {
			a.UserName = *userName
		}
		if alarmTypesStr != nil {
			_ = a.AlarmTypes.Scan(*alarmTypesStr)
		}
		if len(a.AlarmTypes) == 0 {
			a.AlarmTypes = domain.DefaultAlarmTypes
		}
		alarms = append(alarms, &a)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate alarms: %w", rowsErr)
	}
	return alarms, nil
}
