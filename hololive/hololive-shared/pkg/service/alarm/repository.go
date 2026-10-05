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

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Repository struct {
	pool   dbx.Querier
	logger *slog.Logger
}

func NewRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}

func newRepositoryWithQuerier(querier dbx.Querier) *Repository {
	return &Repository{pool: querier}
}

// Add는 방·채널·멤버 구독별 알림 종류를 저장하며 빈 HostID는 전체 채널 구독이다.
// Iris webhook은 방 제목을 싣지 않는다. 지금 bot(v7.0.1)은 모르는 이름을 빈 문자열로 넘기지만, 옛 bot(v7.0.0까지)은
// 방 ID를 RoomName으로 넘겼고 worker를 API보다 먼저 배포하는 동안 옛 API가 계속 보낸다. 방 ID와 같은 이름은 Kakao 방
// 이름이 아니므로 빈 이름으로 저장해 upsert가 기존 Kakao 방 이름과 room_name_updated_at을 방 ID로 덮어쓰지 않게 한다.
func (r *Repository) Add(ctx context.Context, alarm *domain.Alarm) error {
	alarmTypes := alarm.AlarmTypes
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	roomName := alarm.RoomName
	if roomName == alarm.RoomID {
		roomName = ""
	}

	query := mustSQL("repository_0055_01.sql")

	typesValue, err := alarmTypes.Value()
	if err != nil {
		return fmt.Errorf("encode alarm types: %w", err)
	}

	_, err = r.pool.Exec(ctx, query,
		alarm.RoomID, alarm.UserID, alarm.ChannelID,
		roomName, alarm.UserName,
		typesValue, alarm.HostID,
	)
	if err != nil {
		return fmt.Errorf("add alarm: %w", err)
	}

	return nil
}

// Remove는 해당 채팅방의 전체 채널 구독만 삭제한다.
func (r *Repository) Remove(ctx context.Context, roomID, channelID string) error {
	return r.removeSubscription(ctx, roomID, channelID, "")
}

// RemoveHost는 해당 채팅방의 지정한 UNIT B 멤버 구독만 삭제한다.
func (r *Repository) RemoveHost(ctx context.Context, roomID, channelID, hostID string) error {
	if _, ok := mekparkhost.SubscriptionMember(channelID, hostID); !ok {
		return errors.New("remove member subscription: invalid target")
	}

	return r.removeSubscription(ctx, roomID, channelID, hostID)
}

func (r *Repository) removeSubscription(ctx context.Context, roomID, channelID, hostID string) error {
	query := mustSQL("repository_0082_02.sql")

	_, err := r.pool.Exec(ctx, query, roomID, channelID, hostID)
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

	out, err := r.scanAlarms(rows)
	if err != nil {
		return out, fmt.Errorf("scan alarms: %w", err)
	}

	return out, nil
}

// HasChannelSubscriptions는 캐시 유실과 구독 0을 구분하기 위해 영속 구독의 존재를 확인한다.
func (r *Repository) HasChannelSubscriptions(ctx context.Context, channelID string) (bool, error) {
	var exists bool

	if err := r.pool.QueryRow(ctx, mustSQL("repository_channel_subscriptions_exist.sql"), channelID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check channel subscriptions: %w", err)
	}

	return exists, nil
}

func (r *Repository) FindByChannel(ctx context.Context, channelID string) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0117_05.sql")

	rows, err := r.pool.Query(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel: %w", err)
	}
	defer rows.Close()

	out, err := r.scanAlarms(rows)
	if err != nil {
		return out, fmt.Errorf("scan alarms: %w", err)
	}

	return out, nil
}

func (r *Repository) FindByChannelAndType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]*domain.Alarm, error) {
	query := mustSQL("repository_0134_06.sql")

	rows, err := r.pool.Query(ctx, query, channelID, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("find alarms by channel and type: %w", err)
	}
	defer rows.Close()

	out, err := r.scanAlarms(rows)
	if err != nil {
		return out, fmt.Errorf("scan alarms: %w", err)
	}

	return out, nil
}

// GetMemberName은 members 정본의 한국어 표시명(short_korean_name→korean_name)을 돌려준다. 행이 없거나 두 값이 모두
// 비면 빈 문자열이며, 알림 표시 단계가 misc/vtuber_fallback 문구를 쓴다. 과거의 alarms.member_name·호출자 값 대체 단계는
// 제거 조건(두 지표 30일 0, 구독 채널 21개 모두 한국어 표시명 보유)을 확인하고 2026-10-02에 지웠다.
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

	out, err := r.scanAlarms(rows)
	if err != nil {
		return out, fmt.Errorf("scan alarms: %w", err)
	}

	return out, nil
}

// GetAllMemberNames는 알림 구독 채널 중 members에 한국어 표시명이 있는 채널의 표시명을 돌려준다.
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
			return nil, fmt.Errorf("scan alarm row: %w", err)
		}

		alarms = append(alarms, alarm)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate alarms: %w", rowsErr)
	}

	return alarms, nil
}

func scanAlarmRow(rows pgx.Rows) (*domain.Alarm, error) {
	var (
		alarm              domain.Alarm
		roomName, userName *string
		alarmTypesStr      *string
	)

	err := rows.Scan(
		&alarm.ID, &alarm.RoomID, &alarm.UserID, &alarm.ChannelID,
		&roomName, &userName, &alarmTypesStr, &alarm.CreatedAt,
		&alarm.HostID,
	)
	if err != nil {
		return nil, fmt.Errorf("scan alarm: %w", err)
	}

	applyAlarmNullableFields(&alarm, roomName, userName)

	if err := applyAlarmTypes(&alarm, alarmTypesStr); err != nil {
		return nil, fmt.Errorf("apply alarm types: %w", err)
	}

	return &alarm, nil
}

func applyAlarmNullableFields(alarm *domain.Alarm, roomName, userName *string) {
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
