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

package checking

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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

// NewChzzkChecker는 치지직 체커를 생성한다.
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
		logger:      safeLogger(logger),
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

	subscriberMap, err := loadSubscriberRoomsByChannel(ctx, c.cacheClient, youtubeChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("check chzzk streams: load subscriber rooms: %w", err)
	}

	memberNames, err := loadMemberNamesByChannel(ctx, c.cacheClient, youtubeChannelIDs)
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
	eg.SetLimit(defaultLookupConcurrency)

	for youtubeChannelID, chzzkChannelID := range channelMappings {
		job, ok := newChzzkLookupJob(youtubeChannelID, chzzkChannelID, subscriberMap)
		if !ok {
			continue
		}

		eg.Go(func() error {
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

	return roomNotifications(job.subscriberRooms, stream.Channel, stream, 0, "")
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

	startAt := chzzkStartedAtOrFallback(status, detectedAt, title)
	streamIdentity := chzzkStableLiveIdentity(chzzkChannelID, status, startAt, title)
	streamID := fmt.Sprintf("chzzk:%s", streamIdentity)

	channelName := channelNameForMember(youtubeChannelID, memberName, youtubeChannelID)
	liveURL := fmt.Sprintf("https://chzzk.naver.com/live/%s", chzzkChannelID)
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

func chzzkStartedAtOrFallback(status *chzzk.LiveStatusContent, detectedAt time.Time, title string) time.Time {
	for _, field := range []string{"StartedAt", "StartAt", "LiveOpenDate", "OpenDate", "LiveStartAt", "ScheduledStartAt"} {
		if value := reflectTimeField(status, field); !value.IsZero() {
			return value.UTC().Truncate(time.Minute)
		}
	}

	// 플랫폼 시작 시각이 없는 응답에서도 10분마다 새 이벤트가 되지 않도록
	// 감지 일자 단위 fallback을 사용한다. 실제 live id 필드가 있으면 위 경로가 우선된다.
	return detectedAt.UTC().Truncate(24 * time.Hour)
}

func reflectStringField(v any, name string) string {
	field, ok := reflectStructField(v, name)
	if !ok {
		return ""
	}
	return reflectStringValue(field)
}

func reflectStringValue(field reflect.Value) string {
	switch field.Kind() {
	case reflect.String:
		return strings.TrimSpace(field.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflectSignedIntegerString(field)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflectUnsignedIntegerString(field)
	}

	return ""
}

func reflectSignedIntegerString(field reflect.Value) string {
	if field.Int() == 0 {
		return ""
	}
	return strconv.FormatInt(field.Int(), 10)
}

func reflectUnsignedIntegerString(field reflect.Value) string {
	if field.Uint() == 0 {
		return ""
	}
	return strconv.FormatUint(field.Uint(), 10)
}

func reflectTimeField(v any, name string) time.Time {
	field, ok := reflectStructField(v, name)
	if !ok {
		return time.Time{}
	}
	return reflectTimeValue(field)
}

func reflectStructField(v any, name string) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return reflect.Value{}, false
	}

	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	field := elem.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, false
	}
	return field, true
}

func reflectTimeValue(field reflect.Value) time.Time {
	if field.Type() == reflect.TypeFor[time.Time]() {
		parsed, _ := field.Interface().(time.Time)
		return parsed
	}

	if field.Kind() == reflect.String {
		return parseChzzkTime(field.String())
	}

	if parsed := reflectSignedUnixTime(field); !parsed.IsZero() {
		return parsed
	}
	return reflectUnsignedUnixTime(field)
}

func reflectSignedUnixTime(field reflect.Value) time.Time {
	if field.Kind() < reflect.Int || field.Kind() > reflect.Int64 {
		return time.Time{}
	}
	raw := field.Int()
	if raw <= 0 {
		return time.Time{}
	}
	return unixByMagnitude(raw)
}

func reflectUnsignedUnixTime(field reflect.Value) time.Time {
	if field.Kind() < reflect.Uint || field.Kind() > reflect.Uint64 {
		return time.Time{}
	}
	raw := field.Uint()
	if raw <= 0 {
		return time.Time{}
	}
	return unixByMagnitude(int64(raw))
}

func parseChzzkTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}

	if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
		return unixByMagnitude(n)
	}

	return time.Time{}
}

func unixByMagnitude(raw int64) time.Time {
	switch {
	case raw > 1_000_000_000_000_000:
		return time.UnixMicro(raw)
	case raw > 1_000_000_000_000:
		return time.UnixMilli(raw)
	default:
		return time.Unix(raw, 0)
	}
}
