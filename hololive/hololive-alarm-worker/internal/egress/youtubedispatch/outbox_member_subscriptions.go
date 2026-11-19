package youtubedispatch

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

func outboxSubscriberTarget(item *domain.YouTubeNotificationOutbox) (targetKey, title string, err error) {
	if !mekparkhost.SupportsSubscriptions(item.ChannelID) {
		return item.ChannelID, "", nil
	}

	var payload *youtubeoutbox.Video

	if err := jsonv2.Unmarshal([]byte(item.Payload), &payload); err != nil {
		return "", "", fmt.Errorf("decode member subscription title: %w", err)
	}

	if payload == nil {
		return "", "", errors.New("member subscription payload must be an object")
	}

	result := mekparkhost.Identify(item.ChannelID, payload.Title)
	hostIDs := make([]string, 0, len(result.Hosts))

	for _, host := range result.Hosts {
		hostIDs = append(hostIDs, host.ID)
	}

	return item.ChannelID + "|" + strings.Join(hostIDs, ","), payload.Title, nil
}
