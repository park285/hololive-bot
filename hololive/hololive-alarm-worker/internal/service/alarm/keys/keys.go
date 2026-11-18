package keys

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	AlarmChannelRegistryKey           = "alarm:channel_registry"
	AlarmSubscriberCacheEmptyKey      = "alarm:subscriber_cache_empty"
	ChannelSubscribersKeyPrefix       = "alarm:channel_subscribers:"
	ChannelSubscribersCommunityPrefix = "alarm:channel_subscribers:COMMUNITY:"
	ChannelSubscribersShortsPrefix    = "alarm:channel_subscribers:SHORTS:"

	NotifiedKeyPrefix                  = "notified:"
	NotifyClaimKeyPrefix               = "notified:claim:"
	NotifyLogicalClaimKeyPrefix        = "notified:claim:event:"
	UpcomingEventKeyPrefix             = "notified:upcoming:event:"
	RoomScheduleTransitionKeyPrefix    = "notified:schedule:transition:room:"
	LogicalScheduleIndexKeyPrefix      = "notified:schedule:index:"
	LogicalScheduleTransitionKeyPrefix = "notified:schedule:transition:event:"
	NotificationCategoryLiveCatchup    = "live_catchup"
)

func BuildChannelSubscriberKey(channelID string, alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeLive, domain.AlarmTypeBirthday, domain.AlarmTypeAnniversary:
		return ChannelSubscribersKeyPrefix + channelID
	case domain.AlarmTypeCommunity:
		return ChannelSubscribersCommunityPrefix + channelID
	case domain.AlarmTypeShorts:
		return ChannelSubscribersShortsPrefix + channelID
	default:
		return ChannelSubscribersKeyPrefix + channelID
	}
}

func NotifiedKey(streamID string) string {
	return NotifiedKeyPrefix + streamID
}

func NotificationCategory(targetMinutes []int, minutesUntil int) string {
	if minutesUntil == 0 {
		return "live"
	}

	if slices.Contains(targetMinutes, minutesUntil) {
		return "target"
	}

	return strconv.Itoa(minutesUntil)
}

func NormalizeScheduledMinute(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}

func normalizedScheduledUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return NormalizeScheduledMinute(t).Unix()
}

func normalizeTitleForFingerprint(title string) string {
	base := stringutil.NormalizeKey(title)
	if base == "" {
		return ""
	}

	var builder strings.Builder

	builder.Grow(len(base))

	for _, r := range base {
		switch r {
		case '?', '(', ')', '&', ',', ':', ';',
			'\uFF01', '\uFF1F', '\uFF08', '\uFF09', '\uFF06', '\uFF0C', '\uFF1A', '\uFF1B',
			'\u3001', '\u3002', '\u300C', '\u300D', '\u3010', '\u3011':
			continue
		default:
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

func BuildTitleFingerprint(title, streamID string) string {
	normalized := normalizeTitleForFingerprint(title)
	if normalized == "" {
		normalized = normalizeTitleForFingerprint(streamID)
	}

	if normalized == "" {
		normalized = "untitled"
	}

	h := sha256.Sum256([]byte(normalized))

	return hex.EncodeToString(h[:8])
}

func BuildNotifyClaimKey(roomID, streamID string, startScheduled time.Time, category string) string {
	scheduleUnix := normalizedScheduledUnix(startScheduled)
	return fmt.Sprintf("%s%s:%s:%d:%s", NotifyClaimKeyPrefix, roomID, streamID, scheduleUnix, category)
}

func BuildLogicalEventClaimKey(roomID, channelID, streamID, title string, startScheduled time.Time, category string) string {
	scheduleUnix := normalizedScheduledUnix(startScheduled)
	titleFP := BuildTitleFingerprint(title, streamID)

	return fmt.Sprintf("%s%s:%s:%d:%s:%s", NotifyLogicalClaimKeyPrefix, roomID, channelID, scheduleUnix, titleFP, category)
}

func BuildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	scheduleUnix := NormalizeScheduledMinute(startScheduled).Unix()
	titleFP := BuildTitleFingerprint(title, streamID)

	return fmt.Sprintf("%s%s:%s:%d:%s", UpcomingEventKeyPrefix, roomID, channelID, scheduleUnix, titleFP)
}

func BuildRoomScheduleTransitionKey(roomID, streamID string, oldScheduled, newScheduled time.Time) string {
	oldUnix := NormalizeScheduledMinute(oldScheduled).Unix()
	newUnix := NormalizeScheduledMinute(newScheduled).Unix()

	return fmt.Sprintf("%s%s:%s:%d:%d", RoomScheduleTransitionKeyPrefix, roomID, streamID, oldUnix, newUnix)
}

func BuildLogicalScheduleIndexKey(roomID, channelID, streamID, title string) string {
	titleFP := BuildTitleFingerprint(title, streamID)
	return fmt.Sprintf("%s%s:%s:%s", LogicalScheduleIndexKeyPrefix, roomID, channelID, titleFP)
}

func BuildLogicalScheduleTransitionKey(roomID, channelID, streamID, title string, oldScheduled, newScheduled time.Time) string {
	oldUnix := NormalizeScheduledMinute(oldScheduled).Unix()
	newUnix := NormalizeScheduledMinute(newScheduled).Unix()
	titleFP := BuildTitleFingerprint(title, streamID)

	return fmt.Sprintf("%s%s:%s:%s:%d:%d", LogicalScheduleTransitionKeyPrefix, roomID, channelID, titleFP, oldUnix, newUnix)
}

func FormatScheduled(t time.Time) string {
	return NormalizeScheduledMinute(t).UTC().Format(time.RFC3339)
}
