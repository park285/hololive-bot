package checking

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

func (c *YouTubeChecker) eventSubscriberRooms(ctx context.Context, channelID, title string, channelRooms []string) ([]string, error) {
	if !mekparkhost.SupportsSubscriptions(channelID) {
		return channelRooms, nil
	}

	if c.lookupSubscribers == nil {
		return nil, errors.New("member subscription resolver is not configured")
	}

	rooms, err := c.lookupSubscribers(ctx, channelID, title, domain.AlarmTypeLive)
	if err != nil {
		return nil, fmt.Errorf("resolve member subscribers: %w", err)
	}

	return rooms, nil
}

func (c *YouTubeChecker) guardrailSubscriberRooms(ctx context.Context, meta *persistedLiveGuardrailMeta, streamsByChannel map[string][]*domain.Stream) ([]string, error) {
	if !mekparkhost.SupportsSubscriptions(meta.channelID) {
		return meta.rooms, nil
	}

	stream := findYouTubeStreamByID(streamsByChannel[meta.channelID], meta.streamID)
	if stream == nil {
		return nil, errors.New("member subscription guardrail stream is missing")
	}

	return c.eventSubscriberRooms(ctx, meta.channelID, stream.Title, meta.rooms)
}
