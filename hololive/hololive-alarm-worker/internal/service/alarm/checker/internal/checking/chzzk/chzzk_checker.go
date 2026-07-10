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

package chzzk

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

const (
	chzzkTimeBucket = 10 * time.Minute
)

// ChzzkChecker는 치지직 라이브 상태를 조회해 알림 후보를 만든다.
type ChzzkChecker struct {
	cacheClient cache.Client
	chzzkClient *chzzk.Client
	logger      *slog.Logger
}

func NewChzzkChecker(cacheClient cache.Client, chzzkClient *chzzk.Client, logger *slog.Logger) (*ChzzkChecker, error) {
	if cacheClient == nil {
		return nil, errors.New("new chzzk checker: cache service is nil")
	}

	if chzzkClient == nil {
		return nil, errors.New("new chzzk checker: chzzk client is nil")
	}

	return &ChzzkChecker{
		cacheClient: cacheClient,
		chzzkClient: chzzkClient,
		logger:      checking.SafeLogger(logger),
	}, nil
}

// Check는 alarm:chzzk_channels 매핑 기반으로 라이브 알림 후보를 생성한다.
func (c *ChzzkChecker) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	channelMappings, err := c.cacheClient.HGetAll(ctx, sharedalarmkeys.ChzzkChannelMapKey)
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

	subscriberMap, err := checking.LoadSubscriberRoomsByChannel(ctx, c.cacheClient, youtubeChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("check chzzk streams: load subscriber rooms: %w", err)
	}

	memberNames, err := checking.LoadMemberNamesByChannel(ctx, c.cacheClient, youtubeChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("check chzzk streams: load member names: %w", err)
	}

	return c.collectChzzkNotifications(ctx, channelMappings, subscriberMap, memberNames, time.Now().UTC())
}

func (c *ChzzkChecker) collectChzzkNotifications(
	ctx context.Context,
	channelMappings map[string]string,
	subscriberMap map[string][]string,
	memberNames map[string]string,
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0)

	var mu sync.Mutex
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(checking.DefaultLookupConcurrency)

	for youtubeChannelID, chzzkChannelID := range channelMappings {
		job, ok := newChzzkLookupJob(youtubeChannelID, chzzkChannelID, subscriberMap)
		if !ok {
			continue
		}

		panicguard.GoE(eg, c.logger, "chzzk-channel-check", func() error {
			channelNotifications := c.lookupChzzkNotifications(egCtx, job, memberNames, now)
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

func (c *ChzzkChecker) lookupChzzkNotifications(ctx context.Context, job chzzkLookupJob, memberNames map[string]string, now time.Time) []*domain.AlarmNotification {
	liveStatus, liveErr := c.chzzkClient.GetLiveStatus(ctx, job.chzzkChannelID)
	if liveErr != nil {
		c.logger.Warn("Chzzk live status lookup failed",
			slog.String("youtube_channel_id", job.youtubeChannelID),
			slog.String("chzzk_channel_id", job.chzzkChannelID),
			slog.Any("error", liveErr),
		)
		return nil
	}

	if !isChzzkLive(liveStatus) {
		return nil
	}

	stream := buildChzzkLiveStream(job.youtubeChannelID, job.chzzkChannelID, memberNames[job.youtubeChannelID], liveStatus, now)
	if stream == nil {
		return nil
	}

	return checking.RoomNotifications(job.subscriberRooms, stream.Channel, stream, 0, "")
}

func isChzzkLive(status *chzzk.LiveStatusContent) bool {
	if status == nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(status.Status), "OPEN")
}

// buildChzzkLiveDedupKey는 이전 checker-level preclaim 테스트 호환을 위해 남겨둔다.
// 실제 dedup claim은 Notifier가 처리한다.
func buildChzzkLiveDedupKey(chzzkChannelID string, detectedAt time.Time) string {
	bucket := detectedAt.UTC().Truncate(chzzkTimeBucket)
	return fmt.Sprintf("%s%s:%s", sharedalarmkeys.ChzzkLiveNotifiedKeyPrefix, chzzkChannelID, bucket.Format("20060102T1504"))
}

func buildChzzkLiveStream(
	youtubeChannelID string,
	chzzkChannelID string,
	memberName string,
	status *chzzk.LiveStatusContent,
	detectedAt time.Time,
) *domain.Stream {
	youtubeChannelID = strings.TrimSpace(youtubeChannelID)
	chzzkChannelID = strings.TrimSpace(chzzkChannelID)
	if youtubeChannelID == "" || chzzkChannelID == "" {
		return nil
	}

	title := "치지직 라이브"
	if status != nil && strings.TrimSpace(status.LiveTitle) != "" {
		title = strings.TrimSpace(status.LiveTitle)
	}

	startAt := chzzkStartedAtOrFallback(status, detectedAt)
	streamIdentity := chzzkStableLiveIdentity(chzzkChannelID, status, startAt, title)
	streamID := fmt.Sprintf("chzzk:%s", streamIdentity)

	channelName := checking.ChannelNameForMember(youtubeChannelID, memberName, youtubeChannelID)
	liveURL := domain.ChzzkLiveURL(chzzkChannelID)
	link := liveURL

	stream := &domain.Stream{
		ID:             streamID,
		Title:          title,
		ChannelID:      youtubeChannelID,
		ChannelName:    channelName,
		Status:         domain.StreamStatusLive,
		StartScheduled: &startAt,
		StartActual:    &startAt,
		Link:           &link,
		Channel: &domain.Channel{
			ID:   youtubeChannelID,
			Name: channelName,
		},
		ChzzkChannelID: chzzkChannelID,
		ChzzkLiveURL:   liveURL,
		IsChzzkOnly:    true,
	}

	if status != nil {
		viewerCount := status.ConcurrentUserCount
		stream.ViewerCount = &viewerCount
	}

	return stream
}

func chzzkStableLiveIdentity(chzzkChannelID string, status *chzzk.LiveStatusContent, startAt time.Time, title string) string {
	for _, field := range []string{"LiveID", "LiveId", "LiveNo", "LiveNumber", "VideoID", "VideoId", "ID", "Id"} {
		if value := reflectStringField(status, field); value != "" {
			return fmt.Sprintf("%s:%s", chzzkChannelID, value)
		}
	}

	seed := strings.Join([]string{
		chzzkChannelID,
		startAt.UTC().Format("20060102T1504"),
		strings.TrimSpace(title),
		reflectStringField(status, "LiveCategoryValue"),
	}, "|")
	sum := sha256.Sum256([]byte(seed))

	return fmt.Sprintf("%s:fallback:%x", chzzkChannelID, sum[:8])
}

func chzzkStartedAtOrFallback(status *chzzk.LiveStatusContent, detectedAt time.Time) time.Time {
	for _, field := range []string{"StartedAt", "StartAt", "LiveOpenDate", "OpenDate", "LiveStartAt", "ScheduledStartAt"} {
		if value := reflectTimeField(status, field); !value.IsZero() {
			return value.UTC().Truncate(time.Minute)
		}
	}

	// 플랫폼 시작 시각이 없는 응답에서도 10분마다 새 이벤트가 되지 않도록
	// 감지 일자 단위 fallback을 사용한다. 실제 live id 필드가 있으면 위 경로가 우선된다.
	return detectedAt.UTC().Truncate(24 * time.Hour)
}
