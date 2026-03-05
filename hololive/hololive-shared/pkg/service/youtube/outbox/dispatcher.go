package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

// Config: Dispatcher 설정
type Config struct {
	BatchSize      int           // 한 번에 처리할 알림 수
	LockTimeout    time.Duration // 락 타임아웃 (처리 중 상태 유지 시간)
	PollInterval   time.Duration // 폴링 간격
	MaxRetries     int           // 최대 재시도 횟수
	RetryBackoff   time.Duration // 재시도 간격
	CleanupAfter   time.Duration // 완료된 알림 정리 기간
	CleanupEnabled bool          // 정리 활성화 여부
	PerRoomMode    bool          // room 단위 전달 상태 모드 (PR-2 단계적 전환용)
}

// DefaultConfig: 기본 설정
func DefaultConfig() Config {
	return Config{
		BatchSize:      50,
		LockTimeout:    5 * time.Minute,
		PollInterval:   30 * time.Second,
		MaxRetries:     3,
		RetryBackoff:   1 * time.Minute,
		CleanupAfter:   7 * 24 * time.Hour, // 7일
		CleanupEnabled: true,
		PerRoomMode:    false,
	}
}

// Dispatcher: Outbox 알림 발송 처리기
type Dispatcher struct {
	db       *gorm.DB
	cache    cache.Client
	sender   iris.Client
	renderer *template.Renderer
	logger   *slog.Logger
	cfg      Config
	delivery *DeliveryRepository
}

// NewDispatcher: 새 Dispatcher 생성
func NewDispatcher(db *gorm.DB, cacheSvc cache.Client, sender iris.Client, renderer *template.Renderer, logger *slog.Logger, cfg Config) *Dispatcher {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultConfig().BatchSize
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = DefaultConfig().LockTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultConfig().PollInterval
	}

	return &Dispatcher{
		db:       db,
		cache:    cacheSvc,
		sender:   sender,
		renderer: renderer,
		logger:   logger,
		cfg:      cfg,
		delivery: NewDeliveryRepository(db, logger),
	}
}

// Start: 백그라운드 폴링 루프 시작
func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
	if d.cfg.CleanupEnabled {
		go d.cleanupLoop(ctx)
	}
}

// run: 메인 폴링 루프
func (d *Dispatcher) run(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.cfg.PollInterval),
		slog.Int("batch_size", d.cfg.BatchSize))

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("Outbox dispatcher stopped")
			return
		case <-ticker.C:
			d.processOnce(ctx)
		}
	}
}

// processOnce: 한 번의 폴링 사이클
func (d *Dispatcher) processOnce(ctx context.Context) {
	// PENDING 상태인 알림 조회 (락 획득)
	outboxItems, err := d.fetchAndLock(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
		return
	}

	if len(outboxItems) == 0 {
		return
	}

	d.logger.Debug("Processing outbox batch", slog.Int("count", len(outboxItems)))
	if d.cfg.PerRoomMode {
		d.processPerRoomBatch(ctx, outboxItems)
		return
	}

	roomsByChannel := d.collectRoomsByChannel(ctx, outboxItems)
	if len(roomsByChannel) == 0 {
		d.markSentBatch(ctx, collectOutboxIDs(outboxItems))
		return
	}

	groups := d.groupOutboxItems(outboxItems, roomsByChannel)
	for _, group := range groups {
		if len(group.items) == 1 {
			if err := d.processItem(ctx, group.items[0]); err != nil {
				d.logger.Warn("Failed to process outbox item",
					slog.Int64("id", group.items[0].ID),
					slog.String("kind", string(group.items[0].Kind)),
					slog.Any("error", err))
			}
		} else {
			d.processGroupedItems(ctx, group)
		}
	}
}

func (d *Dispatcher) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) {
	roomsByChannel := d.collectRoomsByChannel(ctx, outboxItems)
	d.enqueueDeliveries(ctx, outboxItems, roomsByChannel)
	d.processPendingDeliveries(ctx)
}

func (d *Dispatcher) enqueueDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, roomsByChannel map[string]map[string]bool) {
	for i := range outboxItems {
		item := &outboxItems[i]
		rooms, ok := roomsByChannel[item.ChannelID]
		if !ok || len(rooms) == 0 {
			d.markSent(ctx, item.ID)
			continue
		}

		roomIDs := make([]string, 0, len(rooms))
		for roomID := range rooms {
			roomIDs = append(roomIDs, roomID)
		}

		if err := d.delivery.EnqueueBatch(ctx, item.ID, roomIDs); err != nil {
			d.logger.Warn("Failed to enqueue room deliveries",
				slog.Int64("outbox_id", item.ID),
				slog.Any("error", err))
			d.markFailed(ctx, item.ID, fmt.Sprintf("enqueue delivery rows: %v", err))
			continue
		}

		d.releaseOutboxLock(ctx, item.ID)
	}
}

func (d *Dispatcher) processPendingDeliveries(ctx context.Context) {
	rows, err := d.delivery.FetchAndLock(ctx, d.cfg.BatchSize, d.cfg.LockTimeout)
	if err != nil {
		d.logger.Error("Failed to fetch delivery rows", slog.Any("error", err))
		return
	}
	if len(rows) == 0 {
		return
	}

	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}

	outboxByID, err := d.loadOutboxItemsByIDs(ctx, outboxIDs)
	if err != nil {
		d.logger.Error("Failed to load outbox rows for deliveries", slog.Any("error", err))
		for i := range rows {
			_ = d.delivery.MarkFailed(ctx, rows[i].ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, "load outbox rows")
		}
		return
	}

	successDeliveryIDs := make([]int64, 0, len(rows))
	touchedOutboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		row := rows[i]
		item, ok := outboxByID[row.OutboxID]
		if !ok {
			_ = d.delivery.MarkFailed(ctx, row.ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, "outbox row not found")
			touchedOutboxIDs = append(touchedOutboxIDs, row.OutboxID)
			continue
		}

		message, formatErr := d.formatMessage(ctx, item)
		if formatErr != nil {
			_ = d.delivery.MarkFailed(ctx, row.ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, fmt.Sprintf("format message: %v", formatErr))
			touchedOutboxIDs = append(touchedOutboxIDs, row.OutboxID)
			continue
		}

		if sendErr := d.sender.SendMessage(ctx, row.RoomID, message); sendErr != nil {
			_ = d.delivery.MarkFailed(ctx, row.ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, fmt.Sprintf("send message: %v", sendErr))
			touchedOutboxIDs = append(touchedOutboxIDs, row.OutboxID)
			continue
		}

		successDeliveryIDs = append(successDeliveryIDs, row.ID)
		touchedOutboxIDs = append(touchedOutboxIDs, row.OutboxID)
	}

	if err := d.delivery.MarkSentBatch(ctx, successDeliveryIDs); err != nil {
		d.logger.Error("Failed to mark delivery rows as sent", slog.Any("error", err))
	}

	for _, outboxID := range uniqueInt64s(touchedOutboxIDs) {
		if err := d.delivery.UpdateOutboxAggregateStatus(ctx, outboxID); err != nil {
			d.logger.Warn("Failed to update outbox aggregate status",
				slog.Int64("outbox_id", outboxID),
				slog.Any("error", err))
		}
	}
}

func (d *Dispatcher) loadOutboxItemsByIDs(ctx context.Context, ids []int64) (map[int64]domain.YouTubeNotificationOutbox, error) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return map[int64]domain.YouTubeNotificationOutbox{}, nil
	}

	var rows []domain.YouTubeNotificationOutbox
	if err := d.db.WithContext(ctx).
		Where("id IN ?", uniqueIDs).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load outbox rows by ids: %w", err)
	}

	result := make(map[int64]domain.YouTubeNotificationOutbox, len(rows))
	for i := range rows {
		result[rows[i].ID] = rows[i]
	}
	return result, nil
}

func (d *Dispatcher) releaseOutboxLock(ctx context.Context, id int64) {
	result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ?", id, domain.OutboxStatusPending).
		Update("locked_at", nil)
	if result.Error != nil {
		d.logger.Warn("Failed to release outbox lock",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}
}

// fetchAndLock: PENDING 상태 알림 조회 + 락 획득 (retry 지원)
func (d *Dispatcher) fetchAndLock(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	var items []domain.YouTubeNotificationOutbox
	now := time.Now()
	lockExpiry := now.Add(-d.cfg.LockTimeout)

	// SKIP LOCKED 기반으로 후보 선택 + locked_at 갱신을 한 번에 수행한다.
	if err := d.db.WithContext(ctx).Raw(`
		WITH claim AS (
			SELECT id
			FROM youtube_notification_outbox
			WHERE status = ?
			  AND (locked_at IS NULL OR locked_at < ?)
			  AND next_attempt_at <= ?
			ORDER BY created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE youtube_notification_outbox o
			SET locked_at = ?
			FROM claim
			WHERE o.id = claim.id
			RETURNING o.id, o.kind, o.channel_id, o.content_id, o.payload, o.status,
			          o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, o.error
		)
		SELECT id, kind, channel_id, content_id, payload, status,
		       attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY created_at ASC
	`, domain.OutboxStatusPending, lockExpiry, now, d.cfg.BatchSize, now).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("fetch and lock outbox items: %w", err)
	}

	return items, nil
}

// processItem: 개별 알림 처리
func (d *Dispatcher) processItem(ctx context.Context, item domain.YouTubeNotificationOutbox) error {
	alarmType := item.Kind.ToAlarmType()
	subscribers, err := d.getChannelSubscribers(ctx, item.ChannelID, alarmType)
	if err != nil {
		d.markFailed(ctx, item.ID, fmt.Sprintf("failed to get subscribers: %v", err))
		return fmt.Errorf("failed to get subscribers for channel %s: %w", item.ChannelID, err)
	}

	if len(subscribers) == 0 {
		d.logger.Debug("No subscribers for channel, skipping",
			slog.String("channel_id", item.ChannelID))
		d.markSent(ctx, item.ID)
		return nil
	}

	message, err := d.formatMessage(ctx, item)
	if err != nil {
		d.markFailed(ctx, item.ID, fmt.Sprintf("failed to format message: %v", err))
		return fmt.Errorf("failed to format message for item %d: %w", item.ID, err)
	}

	var sendErrors []string
	roomsSent := 0

	for roomID := range d.groupByRoom(subscribers) {
		if err := d.sender.SendMessage(ctx, roomID, message); err != nil {
			sendErrors = append(sendErrors, fmt.Sprintf("room=%s: %v", roomID, err))
			d.logger.Warn("Failed to send message to room",
				slog.String("room_id", roomID),
				slog.Any("error", err))
		} else {
			roomsSent++
		}
	}

	if len(sendErrors) > 0 && roomsSent == 0 {
		// 모든 발송 실패
		d.markFailed(ctx, item.ID, strings.Join(sendErrors, "; "))
		return fmt.Errorf("all sends failed: %s", strings.Join(sendErrors, "; "))
	}

	// 일부라도 성공하면 완료 처리
	d.markSent(ctx, item.ID)

	d.logger.Info("Outbox notification sent",
		slog.Int64("id", item.ID),
		slog.String("kind", string(item.Kind)),
		slog.String("channel_id", item.ChannelID),
		slog.Int("rooms_sent", roomsSent))

	return nil
}

// getChannelSubscribers: 채널 구독자 목록 조회 (알람 타입별)
func (d *Dispatcher) getChannelSubscribers(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	var channelSubsKey string
	switch alarmType {
	case domain.AlarmTypeCommunity:
		channelSubsKey = "alarm:channel_subscribers:COMMUNITY:" + channelID
	case domain.AlarmTypeShorts:
		channelSubsKey = "alarm:channel_subscribers:SHORTS:" + channelID
	default:
		channelSubsKey = "alarm:channel_subscribers:" + channelID
	}
	members, err := d.cache.SMembers(ctx, channelSubsKey)
	if err != nil {
		return nil, fmt.Errorf("get channel subscribers: %w", err)
	}
	return members, nil
}

// groupByRoom: 구독자 키를 room별로 그룹화
func (d *Dispatcher) groupByRoom(subscribers []string) map[string][]string {
	rooms := make(map[string][]string)

	for _, key := range subscribers {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		roomID := parts[0]
		userID := parts[1]
		rooms[roomID] = append(rooms[roomID], userID)
	}

	return rooms
}

func (d *Dispatcher) formatMessage(ctx context.Context, item domain.YouTubeNotificationOutbox) (string, error) {
	memberName, err := d.getMemberName(ctx, item.ChannelID)
	if err != nil {
		d.logger.Warn("Failed to get member name, using fallback",
			slog.String("channel_id", item.ChannelID),
			slog.Any("error", err))
		memberName = "VTuber"
	} else if memberName == "" {
		memberName = "VTuber"
	}

	templateKey := item.Kind.ToTemplateKey()
	data, err := d.buildTemplateData(memberName, item)
	if err != nil {
		return "", err
	}

	if d.renderer != nil {
		msg, renderErr := d.renderer.Render(ctx, templateKey, item.ChannelID, data)
		if renderErr == nil {
			return d.applySeeMorePadding(msg, item.Kind, data), nil
		}
		d.logger.Warn("Template render failed, using fallback",
			slog.String("template_key", string(templateKey)),
			slog.Any("error", renderErr))
	}

	return d.formatMessageFallback(memberName, item)
}

type templateData struct {
	MemberName  string
	Title       string
	URL         string
	ContentText string
	Milestone   string
	VideoID     string
	PostID      string
}

func (d *Dispatcher) buildTemplateData(memberName string, item domain.YouTubeNotificationOutbox) (templateData, error) {
	data := templateData{MemberName: memberName}

	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var p videoPayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal video payload: %w", err)
		}
		data.Title = p.Title
		data.VideoID = p.VideoID
		if item.Kind == domain.OutboxKindNewShort {
			data.URL = fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
		} else {
			data.URL = fmt.Sprintf("https://youtu.be/%s", p.VideoID)
		}

	case domain.OutboxKindCommunityPost:
		var p communityPayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal community payload: %w", err)
		}
		data.ContentText = p.ContentText
		data.PostID = p.PostID
		data.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)

	case domain.OutboxKindMilestone:
		var p milestonePayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal milestone payload: %w", err)
		}
		data.Milestone = p.Milestone
	}

	return data, nil
}

// applySeeMorePadding: 카카오톡 '전체 보기' 패딩 적용
// - COMMUNITY: 헤더 + 패딩 적용 (본문 길이 가변)
// - VIDEO, SHORTS, MILESTONE: 헤더만 추가, 패딩 미적용 (짧은 알림 → 즉시 노출)
func (d *Dispatcher) applySeeMorePadding(msg string, kind domain.OutboxKind, data templateData) string {
	switch kind {
	case domain.OutboxKindCommunityPost:
		header := fmt.Sprintf("📝 %s 커뮤니티 알림", data.MemberName)
		return util.ApplyKakaoSeeMorePadding(msg, header)
	case domain.OutboxKindNewShort:
		header := fmt.Sprintf("📱 %s 쇼츠 알림", data.MemberName)
		return header + "\n" + msg
	case domain.OutboxKindNewVideo:
		header := fmt.Sprintf("📺 %s 새 영상", data.MemberName)
		return header + "\n" + msg
	case domain.OutboxKindMilestone:
		header := fmt.Sprintf("🎉 %s 마일스톤 알림", data.MemberName)
		return header + "\n" + msg
	default:
		return msg
	}
}

func (d *Dispatcher) formatMessageFallback(memberName string, item domain.YouTubeNotificationOutbox) (string, error) {
	switch item.Kind {
	case domain.OutboxKindNewVideo:
		return d.formatVideoMessage(memberName, item.Payload, false)
	case domain.OutboxKindNewShort:
		return d.formatVideoMessage(memberName, item.Payload, true)
	case domain.OutboxKindCommunityPost:
		return d.formatCommunityMessage(memberName, item.Payload)
	case domain.OutboxKindMilestone:
		return d.formatMilestoneMessage(memberName, item.Payload)
	default:
		return "", fmt.Errorf("unknown outbox kind: %s", item.Kind)
	}
}

// videoPayload: 영상 payload 구조
type videoPayload struct {
	VideoID       string `json:"video_id"`
	Title         string `json:"title"`
	PublishedText string `json:"published_text,omitempty"`
}

// communityPayload: 커뮤니티 payload 구조
type communityPayload struct {
	PostID      string `json:"post_id"`
	ContentText string `json:"content_text"`
}

// milestonePayload: 마일스톤 payload 구조
type milestonePayload struct {
	SubscriberCount int64  `json:"subscriber_count"`
	Milestone       string `json:"milestone"`
}

// formatVideoMessage: 영상 알림 메시지
func (d *Dispatcher) formatVideoMessage(memberName, payload string, isShort bool) (string, error) {
	var p videoPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal video payload: %w", err)
	}

	title := truncateString(p.Title, 50)

	if isShort {
		url := fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
		header := fmt.Sprintf("📱 %s 쇼츠 알림", memberName)
		body := fmt.Sprintf("%s\n%s", title, url)
		return header + "\n" + body, nil
	}

	url := fmt.Sprintf("https://youtu.be/%s", p.VideoID)
	header := fmt.Sprintf("📺 %s 새 영상", memberName)
	body := fmt.Sprintf("%s\n%s", title, url)
	return header + "\n" + body, nil
}

// formatCommunityMessage: 커뮤니티 포스트 알림 메시지
func (d *Dispatcher) formatCommunityMessage(memberName, payload string) (string, error) {
	var p communityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal community payload: %w", err)
	}

	url := fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
	header := fmt.Sprintf("📝 %s 커뮤니티 알림", memberName)
	body := fmt.Sprintf("%s\n\n%s", p.ContentText, url)

	return util.ApplyKakaoSeeMorePadding(body, header), nil
}

// formatMilestoneMessage: 마일스톤 알림 메시지
func (d *Dispatcher) formatMilestoneMessage(memberName, payload string) (string, error) {
	var p milestonePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal milestone payload: %w", err)
	}

	header := fmt.Sprintf("🎉 %s 마일스톤 알림", memberName)
	body := fmt.Sprintf("%s %s 돌파!", memberName, p.Milestone)
	return header + "\n" + body, nil
}

// getMemberName: 멤버 이름 조회 (캐시)
func (d *Dispatcher) getMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := d.cache.HGet(ctx, "alarm:member_names", channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}
	return name, nil
}

// markSent: 발송 완료 처리
func (d *Dispatcher) markSent(ctx context.Context, id int64) {
	d.markSentBatch(ctx, []int64{id})
}

const markSentBatchChunkSize = 500

func (d *Dispatcher) markSentBatch(ctx context.Context, ids []int64) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return
	}

	now := time.Now()
	for start := 0; start < len(uniqueIDs); start += markSentBatchChunkSize {
		end := start + markSentBatchChunkSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		chunk := uniqueIDs[start:end]

		result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id IN ? AND status = ?", chunk, domain.OutboxStatusPending).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"sent_at":   now,
				"locked_at": nil,
				"error":     "",
			})
		if result.Error != nil {
			d.logger.Error("Failed to mark outbox items as sent",
				slog.Int("batch_size", len(chunk)),
				slog.Any("error", result.Error))
		}
	}
}

// markFailed: 발송 실패 처리 (retry 지원)
func (d *Dispatcher) markFailed(ctx context.Context, id int64, errMsg string) {
	var item domain.YouTubeNotificationOutbox
	if err := d.db.WithContext(ctx).First(&item, id).Error; err != nil {
		d.logger.Warn("Failed to fetch outbox item for retry", slog.Int64("id", id), slog.Any("error", err))
		return
	}

	newAttemptCount := item.AttemptCount + 1

	if newAttemptCount >= d.cfg.MaxRetries {
		result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"status":        domain.OutboxStatusFailed,
				"locked_at":     nil,
				"attempt_count": newAttemptCount,
				"error":         truncateString(errMsg, 500),
			})
		if result.Error != nil {
			d.logger.Error("Failed to mark outbox item as permanently failed",
				slog.Int64("id", id),
				slog.Any("error", result.Error))
		}
		d.logger.Warn("Outbox item permanently failed after max retries",
			slog.Int64("id", id),
			slog.Int("attempts", newAttemptCount))
		return
	}

	nextAttempt := time.Now().Add(d.cfg.RetryBackoff * time.Duration(newAttemptCount))
	result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"locked_at":       nil,
			"attempt_count":   newAttemptCount,
			"next_attempt_at": nextAttempt,
			"error":           truncateString(errMsg, 500),
		})
	if result.Error != nil {
		d.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}

	d.logger.Info("Outbox item scheduled for retry",
		slog.Int64("id", id),
		slog.Int("attempt", newAttemptCount),
		slog.Time("next_attempt", nextAttempt))
}

// cleanupLoop: 오래된 완료 알림 정리 루프
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.cleanup(ctx)
		}
	}
}

// cleanup: 오래된 완료 알림 삭제
func (d *Dispatcher) cleanup(ctx context.Context) {
	cutoff := time.Now().Add(-d.cfg.CleanupAfter)

	result := d.db.WithContext(ctx).
		Where("status = ? AND sent_at < ?", domain.OutboxStatusSent, cutoff).
		Delete(&domain.YouTubeNotificationOutbox{})

	if result.Error != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", result.Error))
		return
	}

	if result.RowsAffected > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", result.RowsAffected))
	}
}

// outboxItemGroup: Outbox 알림 그룹 (동일 Room + Channel + Kind 묶음)
type outboxItemGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	items     []domain.YouTubeNotificationOutbox
}

func (d *Dispatcher) groupOutboxItems(items []domain.YouTubeNotificationOutbox, roomsByChannel map[string]map[string]bool) []*outboxItemGroup {
	if len(items) == 0 {
		return nil
	}

	groups := make([]*outboxItemGroup, 0)
	index := make(map[string]int)

	for i := range items {
		item := &items[i]
		rooms, ok := roomsByChannel[item.ChannelID]
		if !ok || len(rooms) == 0 {
			continue
		}

		for roomID := range rooms {
			key := fmt.Sprintf("%s|%s|%s", roomID, item.ChannelID, item.Kind)
			if idx, exists := index[key]; exists {
				groups[idx].items = append(groups[idx].items, *item)
			} else {
				groups = append(groups, &outboxItemGroup{
					roomID:    roomID,
					channelID: item.ChannelID,
					kind:      item.Kind,
					items:     []domain.YouTubeNotificationOutbox{*item},
				})
				index[key] = len(groups) - 1
			}
		}
	}

	return groups
}

func (d *Dispatcher) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	seen := make(map[string]bool)

	for i := range items {
		item := &items[i]
		if seen[item.ChannelID] {
			continue
		}
		seen[item.ChannelID] = true

		alarmType := item.Kind.ToAlarmType()
		subscribers, err := d.getChannelSubscribers(ctx, item.ChannelID, alarmType)
		if err != nil {
			d.logger.Warn("Failed to get subscribers for channel",
				slog.String("channel_id", item.ChannelID),
				slog.Any("error", err))
			continue
		}

		if len(subscribers) == 0 {
			continue
		}

		rooms := d.groupByRoom(subscribers)
		roomSet := make(map[string]bool)
		for roomID := range rooms {
			roomSet[roomID] = true
		}
		result[item.ChannelID] = roomSet
	}

	return result
}

func (d *Dispatcher) processGroupedItems(ctx context.Context, group *outboxItemGroup) {
	if len(group.items) == 0 {
		return
	}

	memberName, err := d.getMemberName(ctx, group.channelID)
	if err != nil {
		d.logger.Warn("Failed to get member name, using fallback",
			slog.String("channel_id", group.channelID),
			slog.Any("error", err))
		memberName = "VTuber"
	} else if memberName == "" {
		memberName = "VTuber"
	}

	message, err := d.formatGroupedMessage(ctx, memberName, group.channelID, group.kind, group.items)
	if err != nil {
		d.logger.Warn("Failed to format grouped message",
			slog.String("channel_id", group.channelID),
			slog.Any("error", err))
		for i := range group.items {
			d.markFailed(ctx, group.items[i].ID, fmt.Sprintf("format error: %v", err))
		}
		return
	}

	if err := d.sender.SendMessage(ctx, group.roomID, message); err != nil {
		d.logger.Warn("Failed to send grouped message",
			slog.String("room_id", group.roomID),
			slog.Any("error", err))
		for i := range group.items {
			d.markFailed(ctx, group.items[i].ID, fmt.Sprintf("send error: %v", err))
		}
		return
	}

	d.markSentBatch(ctx, collectOutboxIDs(group.items))

	d.logger.Info("Outbox grouped notification sent",
		slog.String("kind", string(group.kind)),
		slog.String("channel_id", group.channelID),
		slog.String("room_id", group.roomID),
		slog.Int("count", len(group.items)))
}

func collectOutboxIDs(items []domain.YouTubeNotificationOutbox) []int64 {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	return ids
}

func uniqueInt64s(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// truncateString: 문자열 길이 제한
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

type groupedItemData struct {
	Title       string
	ContentText string
	URL         string
}

type groupedTemplateData struct {
	MemberName string
	Count      int
	Items      []groupedItemData
}

func (d *Dispatcher) formatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to format")
	}

	if d.renderer == nil {
		return "", fmt.Errorf("renderer is nil")
	}

	templateKey, header := d.getGroupedTemplateKeyAndHeader(memberName, kind, len(items))
	data := d.buildGroupedTemplateData(memberName, kind, items)

	msg, err := d.renderer.Render(ctx, templateKey, channelID, data)
	if err != nil {
		return "", fmt.Errorf("render grouped template %s: %w", templateKey, err)
	}

	return util.ApplyKakaoSeeMorePadding(msg, header), nil
}

func (d *Dispatcher) getGroupedTemplateKeyAndHeader(memberName string, kind domain.OutboxKind, count int) (domain.TemplateKey, string) {
	switch kind {
	case domain.OutboxKindNewVideo:
		return domain.TemplateKeyOutboxVideoGroup, fmt.Sprintf("📺 %s 새 영상 (%d개)", memberName, count)
	case domain.OutboxKindNewShort:
		return domain.TemplateKeyOutboxShortsGroup, fmt.Sprintf("📱 %s 쇼츠 알림 (%d개)", memberName, count)
	case domain.OutboxKindCommunityPost:
		return domain.TemplateKeyOutboxCommunityGroup, fmt.Sprintf("📝 %s 커뮤니티 알림 (%d개)", memberName, count)
	default:
		return domain.TemplateKeyOutboxVideoGroup, fmt.Sprintf("🔔 %s 알림 (%d개)", memberName, count)
	}
}

func (d *Dispatcher) buildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) groupedTemplateData {
	data := groupedTemplateData{
		MemberName: memberName,
		Count:      len(items),
		Items:      make([]groupedItemData, 0, len(items)),
	}

	for i := range items {
		item := &items[i]
		itemData := groupedItemData{}

		switch item.Kind {
		case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
			var p videoPayload
			if err := json.Unmarshal([]byte(item.Payload), &p); err == nil {
				itemData.Title = p.Title
				if item.Kind == domain.OutboxKindNewShort {
					itemData.URL = fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
				} else {
					itemData.URL = fmt.Sprintf("https://youtu.be/%s", p.VideoID)
				}
			}
		case domain.OutboxKindCommunityPost:
			var p communityPayload
			if err := json.Unmarshal([]byte(item.Payload), &p); err == nil {
				itemData.ContentText = p.ContentText
				itemData.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
			}
		}

		data.Items = append(data.Items, itemData)
	}

	return data
}

// ProcessOnceForTest: 테스트용 - 한 번의 폴링 사이클 실행
func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}
