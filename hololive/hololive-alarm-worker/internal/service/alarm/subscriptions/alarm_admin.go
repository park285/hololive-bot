package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	entries, err := as.alarmRepository.ListAlarmEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list alarm entries: %w", err)
	}

	if len(entries) == 0 {
		return []*domain.AlarmEntry{}, nil
	}

	channelIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.RoomName == "" {
			entry.RoomName = entry.RoomID
		}

		channelIDs = append(channelIDs, entry.ChannelID)
	}

	memberNames, err := as.memberDisplayNames(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve alarm entry member names: %w", err)
	}

	for _, entry := range entries {
		entry.MemberName = memberNames[entry.ChannelID]
	}

	return entries, nil
}

func (as *AlarmService) CountAlarmEntries(ctx context.Context) (int, error) {
	count, err := as.alarmRepository.CountAlarmEntries(ctx)
	if err != nil {
		return 0, fmt.Errorf("count alarm entries: %w", err)
	}

	return count, nil
}

func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return errors.New("set room name: room id is required")
	}

	roomName = strings.TrimSpace(roomName)
	if roomName == "" {
		if err := as.alarmRepository.DeleteRoomDisplayName(ctx, roomID); err != nil {
			return fmt.Errorf("clear room name: %w", err)
		}

		return nil
	}

	if err := as.alarmRepository.SetRoomDisplayName(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}
