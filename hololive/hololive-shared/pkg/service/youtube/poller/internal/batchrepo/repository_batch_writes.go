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

package batchrepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	yttimestamp "github.com/kapu/hololive-shared/internal/service/youtube/timestamp"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxBatchRepository) batchUpsertVideos(ctx context.Context, tx batchDB, videos []*domain.YouTubeVideo) error {
	if len(videos) == 0 {
		return nil
	}

	for start := 0; start < len(videos); start += PollerBatchMaxSize {
		end := min(start+PollerBatchMaxSize, len(videos))
		chunk := videos[start:end]
		if err := r.upsertVideosChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *PgxBatchRepository) upsertVideosChunk(ctx context.Context, tx batchDB, videos []*domain.YouTubeVideo) error {
	now := time.Now()
	args := make([]any, 0, len(videos)*12)
	var sb strings.Builder
	sb.WriteString(mustSQL("repository_batch_writes_0054_01.sql"))

	for i, video := range videos {
		publishedAt := yttimestamp.NormalizePtr(video.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			video.VideoID,
			video.ChannelID,
			video.Title,
			video.Thumbnail,
			video.Duration,
			video.PublishedText,
			publishedAt,
			video.IsShort,
			video.IsLiveReplay,
			video.ViewCount,
			now,
			now,
		)
	}

	sb.WriteString(mustSQL("repository_batch_writes_0083_02.sql"))

	if _, err := dbx.ExecSQL(ctx, tx, fmt.Sprintf("exec video upsert chunk (%d rows)", len(videos)), sb.String(), args...); err != nil {
		return fmt.Errorf("exec video upsert chunk (%d rows): %w", len(videos), err)
	}

	return nil
}

func (r *PgxBatchRepository) batchUpsertCommunityPosts(ctx context.Context, tx batchDB, posts []*domain.YouTubeCommunityPost) error {
	if len(posts) == 0 {
		return nil
	}

	for start := 0; start < len(posts); start += PollerBatchMaxSize {
		end := min(start+PollerBatchMaxSize, len(posts))
		chunk := posts[start:end]
		if err := r.upsertCommunityPostsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *PgxBatchRepository) upsertCommunityPostsChunk(ctx context.Context, tx batchDB, posts []*domain.YouTubeCommunityPost) error {
	now := time.Now()
	args := make([]any, 0, len(posts)*13)
	var sb strings.Builder
	sb.WriteString(mustSQL("repository_batch_writes_0117_03.sql"))

	for i, post := range posts {
		publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			post.PostID,
			post.ChannelID,
			post.AuthorName,
			post.AuthorPhoto,
			post.ContentText,
			post.PublishedText,
			publishedAt,
			post.LikeCount,
			post.CommentCount,
			post.Images,
			post.AttachedVideo,
			now,
			now,
		)
	}

	sb.WriteString(mustSQL("repository_batch_writes_0147_04.sql"))

	if _, err := dbx.ExecSQL(ctx, tx, fmt.Sprintf("exec community post upsert chunk (%d rows)", len(posts)), sb.String(), args...); err != nil {
		return fmt.Errorf("exec community post upsert chunk (%d rows): %w", len(posts), err)
	}

	return nil
}

func (r *PgxBatchRepository) BatchInsertNotifications(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(notifications) == 0 {
		return nil
	}

	for start := 0; start < len(notifications); start += PollerBatchMaxSize {
		end := min(start+PollerBatchMaxSize, len(notifications))
		chunk := notifications[start:end]
		if err := r.insertNotificationsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *PgxBatchRepository) insertNotificationsChunk(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox) error {
	completedSentAtByIdentity, reactivationRows, err := prepareNotificationInsertChunk(ctx, tx, notifications)
	if err != nil {
		return err
	}

	activeNotifications := filterCompletedNotifications(notifications, completedSentAtByIdentity)
	if len(activeNotifications) == 0 {
		return nil
	}

	now := time.Now()
	for _, chunk := range notificationChunksByKind(activeNotifications) {
		if err := r.insertNotificationsSameKindChunk(ctx, tx, chunk, now); err != nil {
			return err
		}
	}
	if err := rearmFailedDeliveryRows(ctx, tx, collectFailedNotificationOutboxIDs(reactivationRows), now); err != nil {
		return fmt.Errorf("rearm failed delivery rows: %w", err)
	}

	return nil
}

func notificationChunksByKind(notifications []*domain.YouTubeNotificationOutbox) [][]*domain.YouTubeNotificationOutbox {
	chunks := make([][]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	chunkByKind := make(map[domain.OutboxKind]int)
	seenByIdentity := make(map[string]struct{}, len(notifications))
	for _, notification := range notifications {
		if notification == nil {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, notification.ContentID)
		if _, ok := seenByIdentity[identityKey]; ok {
			continue
		}
		seenByIdentity[identityKey] = struct{}{}
		idx, ok := chunkByKind[notification.Kind]
		if !ok {
			chunkByKind[notification.Kind] = len(chunks)
			chunks = append(chunks, make([]*domain.YouTubeNotificationOutbox, 0, 1))
			idx = len(chunks) - 1
		}
		chunks[idx] = append(chunks[idx], notification)
	}
	return chunks
}

func (r *PgxBatchRepository) insertNotificationsSameKindChunk(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox, now time.Time) error {
	if len(notifications) == 0 {
		return nil
	}

	kind := notifications[0].Kind
	args := make([]any, 0, len(notifications)*8)
	var sb strings.Builder
	sb.WriteString(mustSQL("repository_batch_writes_0234_05.sql"))

	for i, notification := range notifications {
		appendNotificationInsertArgs(&sb, &args, i, notification, now)
	}

	sb.WriteString(mustSQL("repository_batch_writes_0244_06.sql"))

	rowsAffected, err := dbx.ExecSQL(ctx, tx, fmt.Sprintf("exec notification insert chunk (%d rows)", len(notifications)), sb.String(), args...)
	if err != nil {
		observeOutboxInsert(kind, "error", int64(len(notifications)))
		return fmt.Errorf("exec notification insert chunk (%d rows): %w", len(notifications), err)
	}
	observeOutboxInsert(kind, "success", rowsAffected)
	observeOutboxInsert(kind, "conflict", int64(len(notifications))-rowsAffected)

	return nil
}

func prepareNotificationInsertChunk(
	ctx context.Context,
	tx batchDB,
	notifications []*domain.YouTubeNotificationOutbox,
) (map[string]time.Time, []failedNotificationOutboxRow, error) {
	if err := validateNotificationDedupeKeys(notifications); err != nil {
		return nil, nil, err
	}

	completedSentAtByIdentity, err := loadCompletedNotificationSentAtByIdentity(ctx, tx, notifications)
	if err != nil {
		return nil, nil, fmt.Errorf("load completed notification sent state: %w", err)
	}
	failedRows, err := loadFailedNotificationOutboxRows(ctx, tx, notifications)
	if err != nil {
		return nil, nil, fmt.Errorf("load failed outbox rows: %w", err)
	}

	completedFailedRows, reactivationRows := partitionFailedNotificationOutboxRows(failedRows, completedSentAtByIdentity)
	if err := finalizeCompletedFailedNotificationRows(ctx, tx, completedFailedRows, completedSentAtByIdentity); err != nil {
		return nil, nil, fmt.Errorf("finalize completed failed notification rows: %w", err)
	}
	return completedSentAtByIdentity, reactivationRows, nil
}

func validateNotificationDedupeKeys(notifications []*domain.YouTubeNotificationOutbox) error {
	for i, notification := range notifications {
		if _, err := notification.DedupeKey(); err != nil {
			return fmt.Errorf("validate notification dedupe key at index %d: %w", i, err)
		}
	}
	return nil
}

func appendNotificationInsertArgs(
	sb *strings.Builder,
	args *[]any,
	index int,
	notification *domain.YouTubeNotificationOutbox,
	now time.Time,
) {
	if index > 0 {
		sb.WriteByte(',')
	}

	status := notification.Status
	if status == "" {
		status = domain.OutboxStatusPending
	}
	nextAttemptAt := notification.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = now
	}
	createdAt := notification.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?)")
	*args = append(
		*args,
		notification.Kind,
		notification.ChannelID,
		notification.ContentID,
		notification.Payload,
		status,
		notification.AttemptCount,
		nextAttemptAt,
		createdAt,
	)
}

func (r *PgxBatchRepository) upsertWatermark(ctx context.Context, tx batchDB, watermark *domain.YouTubeContentWatermark) error {
	if watermark == nil {
		return nil
	}

	now := time.Now()
	if _, err := dbx.ExecSQL(ctx, tx, "exec watermark upsert", mustSQL("repository_batch_writes_0347_07.sql"),
		watermark.ChannelID,
		watermark.WatermarkType,
		watermark.Initialized,
		watermark.LastContentID,
		now,
	); err != nil {
		return fmt.Errorf("exec watermark upsert: %w", err)
	}

	return nil
}
