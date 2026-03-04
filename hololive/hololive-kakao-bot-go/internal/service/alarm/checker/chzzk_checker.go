package checker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

const (
	chzzkTimeBucket = 10 * time.Minute
)

// ChzzkChecker는 치지직 라이브 상태를 조회해 알림 후보를 만든다.
type ChzzkChecker struct {
	cacheSvc    cache.Client
	chzzkClient *chzzk.Client
	logger      *slog.Logger
}

// NewChzzkChecker는 치지직 체커를 생성한다.
func NewChzzkChecker(cacheSvc cache.Client, chzzkClient *chzzk.Client, logger *slog.Logger) (*ChzzkChecker, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("new chzzk checker: cache service is nil")
	}
	if chzzkClient == nil {
		return nil, fmt.Errorf("new chzzk checker: chzzk client is nil")
	}
	return &ChzzkChecker{
		cacheSvc:    cacheSvc,
		chzzkClient: chzzkClient,
		logger:      safeLogger(logger),
	}, nil
}

// Check는 alarm:chzzk_channels 매핑 기반으로 라이브 알림 후보를 생성한다.
func (c *ChzzkChecker) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	channelMappings, err := c.cacheSvc.HGetAll(ctx, notification.ChzzkChannelMapKey)
	if err != nil {
		return nil, fmt.Errorf("check chzzk streams: read channel mappings: %w", err)
	}
	if len(channelMappings) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	youtubeChannelIDs := make([]string, 0, len(channelMappings))
	for youtubeChannelID := range channelMappings {
		youtubeChannelIDs = append(youtubeChannelIDs, youtubeChannelID)
	}

	subscriberMap, err := loadSubscriberRoomsByChannel(ctx, c.cacheSvc, youtubeChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("check chzzk streams: load subscriber rooms: %w", err)
	}

	now := time.Now().UTC()
	notifications := make([]*domain.AlarmNotification, 0)
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(defaultLookupConcurrency)

	for youtubeChannelID, chzzkChannelID := range channelMappings {
		youtubeChannelID := youtubeChannelID
		chzzkChannelID := chzzkChannelID
		subscriberRooms := subscriberMap[youtubeChannelID]
		if len(subscriberRooms) == 0 {
			continue
		}

		eg.Go(func() error {
			liveStatus, liveErr := c.chzzkClient.GetLiveStatus(egCtx, chzzkChannelID)
			if liveErr != nil {
				c.logger.Warn("Chzzk live status lookup failed",
					slog.String("youtube_channel_id", youtubeChannelID),
					slog.String("chzzk_channel_id", chzzkChannelID),
					slog.Any("error", liveErr),
				)
				return nil
			}
			if !isChzzkLive(liveStatus) {
				return nil
			}

			dedupKey := buildChzzkLiveDedupKey(chzzkChannelID, now)
			claimed, claimErr := c.cacheSvc.SetNX(egCtx, dedupKey, "1", sharedconstants.CacheTTL.NotificationSent)
			if claimErr != nil {
				return fmt.Errorf("check chzzk streams: claim dedup key %s: %w", dedupKey, claimErr)
			}
			if !claimed {
				return nil
			}

			stream := buildChzzkLiveStream(youtubeChannelID, chzzkChannelID, liveStatus, now)
			channelNotifications := roomNotifications(subscriberRooms, stream.Channel, stream, 0, "")
			if len(channelNotifications) == 0 {
				return nil
			}

			mu.Lock()
			notifications = append(notifications, channelNotifications...)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("check chzzk streams: wait workers: %w", err)
	}

	return notifications, nil
}

func isChzzkLive(status *chzzk.LiveStatusContent) bool {
	if status == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(status.Status), "OPEN")
}

func buildChzzkLiveDedupKey(chzzkChannelID string, detectedAt time.Time) string {
	bucket := detectedAt.UTC().Truncate(chzzkTimeBucket)
	return fmt.Sprintf("%s%s:%s", notification.ChzzkLiveNotifiedKeyPrefix, chzzkChannelID, bucket.Format("20060102T1504"))
}

func buildChzzkLiveStream(
	youtubeChannelID string,
	chzzkChannelID string,
	status *chzzk.LiveStatusContent,
	detectedAt time.Time,
) *domain.Stream {
	startAt := detectedAt.UTC().Truncate(chzzkTimeBucket)
	streamID := fmt.Sprintf("chzzk:%s:%s", chzzkChannelID, startAt.Format("20060102T1504"))

	title := "치지직 라이브"
	if status != nil && strings.TrimSpace(status.LiveTitle) != "" {
		title = strings.TrimSpace(status.LiveTitle)
	}

	stream := &domain.Stream{
		ID:             streamID,
		Title:          title,
		ChannelID:      youtubeChannelID,
		Status:         domain.StreamStatusLive,
		StartScheduled: &startAt,
		StartActual:    &startAt,
		Channel: &domain.Channel{
			ID:   youtubeChannelID,
			Name: youtubeChannelID,
		},
		ChzzkChannelID: chzzkChannelID,
		ChzzkLiveURL:   fmt.Sprintf("https://chzzk.naver.com/live/%s", chzzkChannelID),
		IsChzzkOnly:    true,
	}

	if status != nil {
		stream.ChannelName = strings.TrimSpace(status.LiveCategoryValue)
		viewerCount := status.ConcurrentUserCount
		stream.ViewerCount = &viewerCount
	}
	if strings.TrimSpace(stream.ChannelName) == "" {
		stream.ChannelName = chzzkChannelID
	}

	return stream
}
