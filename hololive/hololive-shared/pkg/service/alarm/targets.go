package alarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func LookupChannelSubscribersByType(
	ctx context.Context,
	cacheSvc cache.Client,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: cache service is nil")
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	key := sharedalarmkeys.BuildChannelSubscriberKey(normalizedChannelID, alarmType)
	subscribers, err := cacheSvc.SMembers(ctx, key)
	if err != nil {
		return nil, fmt.Errorf(
			"lookup channel subscribers by type: channel %s type %s: %w",
			normalizedChannelID,
			alarmType,
			err,
		)
	}

	return subscribers, nil
}
