package alarmhttp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func (c *Client) LiveSubscriberRooms(ctx context.Context, channelID string) ([]string, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, errors.New("alarm-api: channel_id is required")
	}

	resp, err := c.getJSON[contractsalarm.LiveRoomsResponse](ctx, contractsalarm.LiveRoomsPath(channelID))
	if err != nil {
		return nil, fmt.Errorf("live subscriber rooms: %w", err)
	}

	if resp.RoomIDs == nil {
		return nil, errors.New("live subscriber rooms: missing room_ids")
	}

	return resp.RoomIDs, nil
}
