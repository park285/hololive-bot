// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}

// room_id + channel_id 기준 unique (방 기반 시스템)
func (r *Repository) Add(ctx context.Context, alarm *domain.Alarm) error {
	alarmTypes := alarm.AlarmTypes
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	query := mustSQL("repository_0055_01.sql")

	typesValue, err := alarmTypes.Value()
	if err != nil {
		return fmt.Errorf("encode alarm types: %w", err)
	}
	_, err = r.pool.Exec(ctx, query,
		alarm.RoomID, alarm.UserID, alarm.ChannelID,
		alarm.MemberName, alarm.RoomName, alarm.UserName,
		typesValue,
	)
	if err != nil {
		return fmt.Errorf("add alarm: %w", err)
	}
	return nil
}

func (r *Repository) Remove(ctx context.Context, roomID, channelID string) error {
	query := mustSQL("repository_0082_02.sql")
	_, err := r.pool.Exec(ctx, query, roomID, channelID)
	if err != nil {
		return fmt.Errorf("remove alarm: %w", err)
	}
	return nil
}

func (r *Repository) ClearByRoom(ctx context.Context, roomID string) (int64, error) {
	query := mustSQL("repository_0091_03.sql")
	cmdTag, err := r.pool.Exec(ctx, query, roomID)
	if err != nil {
		return 0, fmt.Errorf("clear alarms: %w", err)
	}
	return cmdTag.RowsAffected(), nil
}

func (r *Repository) FindByRoom(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0100_04.sql")

	rows, err := r.pool.Query(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("find alarms by room: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

func (r *Repository) FindByChannel(ctx context.Context, channelID string) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0117_05.sql")

	rows, err := r.pool.Query(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

func (r *Repository) FindByChannelAndType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0134_06.sql")

	rows, err := r.pool.Query(ctx, query, channelID, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel and type: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

func (r *Repository) GetMemberName(ctx context.Context, channelID string) (string, error) {
	query := mustSQL("repository_0155_07.sql")

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

func (r *Repository) LoadAll(ctx context.Context) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0191_08.sql")

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load all alarms: %w", err)
	}
	defer rows.Close()

	return r.scanAlarms(rows)
}

func (r *Repository) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	query := mustSQL("repository_0207_09.sql")

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

func (r *Repository) GetAllMemberNames(ctx context.Context) (map[string]string, error) {
	query := mustSQL("repository_0231_10.sql")

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
		alarm, err := scanAlarmRow(rows)
		if err != nil {
			return nil, err
		}
		alarms = append(alarms, alarm)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate alarms: %w", rowsErr)
	}
	return alarms, nil
}

func scanAlarmRow(rows pgx.Rows) (*domain.Alarm, error) {
	var alarm domain.Alarm
	var memberName, roomName, userName *string
	var alarmTypesStr *string

	err := rows.Scan(
		&alarm.ID, &alarm.RoomID, &alarm.UserID, &alarm.ChannelID,
		&memberName, &roomName, &userName, &alarmTypesStr, &alarm.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan alarm: %w", err)
	}

	applyAlarmNullableFields(&alarm, memberName, roomName, userName)
	if err := applyAlarmTypes(&alarm, alarmTypesStr); err != nil {
		return nil, err
	}
	return &alarm, nil
}

func applyAlarmNullableFields(alarm *domain.Alarm, memberName, roomName, userName *string) {
	if memberName != nil {
		alarm.MemberName = *memberName
	}
	if roomName != nil {
		alarm.RoomName = *roomName
	}
	if userName != nil {
		alarm.UserName = *userName
	}
}

func applyAlarmTypes(alarm *domain.Alarm, alarmTypesStr *string) error {
	if alarmTypesStr != nil {
		if err := alarm.AlarmTypes.Scan(*alarmTypesStr); err != nil {
			return fmt.Errorf("scan alarm types: %w", err)
		}
	}
	if len(alarm.AlarmTypes) == 0 {
		alarm.AlarmTypes = domain.DefaultAlarmTypes
	}
	return nil
}
