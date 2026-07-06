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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type pgBroadcastHistoryRepository struct {
	pool broadcastHistoryDB
}

const (
	defaultBroadcastHistoryLimit = 8
	maxBroadcastHistoryLimit     = 20
	broadcastHistoryPageSize     = 500
)

type broadcastHistoryDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func NewPgBroadcastHistoryRepository(postgres database.Client) BroadcastHistoryRepository {
	if postgres == nil || postgres.GetPool() == nil {
		return nil
	}
	return &pgBroadcastHistoryRepository{pool: postgres.GetPool()}
}

func (r *pgBroadcastHistoryRepository) ListEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery) ([]handlercore.BroadcastHistoryEntry, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("broadcast history repository not configured")
	}

	limit := normalizeBroadcastHistoryLimit(query.Limit)
	since := broadcastHistorySinceCursor(query)
	return r.collectEndedBroadcasts(ctx, query, since, limit)
}

func (r *pgBroadcastHistoryRepository) collectEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery, since *time.Time, limit int) ([]handlercore.BroadcastHistoryEntry, error) {
	entries := make([]handlercore.BroadcastHistoryEntry, 0, limit)
	var cursorAt *time.Time
	cursorVideoID := ""
	for len(entries) < limit {
		page, nextCursorAt, nextCursorVideoID, err := r.listEndedBroadcastPage(ctx, query.ChannelID, since, cursorAt, cursorVideoID)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		entries = appendMatchingBroadcastHistoryEntries(entries, page, query, limit)
		if len(page) < broadcastHistoryPageSize {
			break
		}
		cursorAt, cursorVideoID = nextCursorAt, nextCursorVideoID
	}
	return entries, nil
}

func broadcastHistorySinceCursor(query *handlercore.BroadcastHistoryQuery) *time.Time {
	if query.IncludeAll || query.Since.IsZero() {
		return nil
	}
	value := query.Since.UTC()
	return &value
}

func appendMatchingBroadcastHistoryEntries(entries, page []handlercore.BroadcastHistoryEntry, query *handlercore.BroadcastHistoryQuery, limit int) []handlercore.BroadcastHistoryEntry {
	for i := range page {
		if !broadcastHistoryEntryMatches(query, &page[i]) {
			continue
		}
		entries = append(entries, page[i])
		if len(entries) >= limit {
			break
		}
	}
	return entries
}

func (r *pgBroadcastHistoryRepository) listEndedBroadcastPage(ctx context.Context, channelID string, since, cursorAt *time.Time, cursorVideoID string) ([]handlercore.BroadcastHistoryEntry, *time.Time, string, error) {
	rows, err := r.pool.Query(ctx, broadcastHistorySelectSQL+`
		WHERE s.status = 'ENDED'
		  AND ($1 = '' OR s.channel_id = $1)
		  AND ($2::timestamptz IS NULL OR COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) >= $2)
		  AND ($3::timestamptz IS NULL
		       OR COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) < $3
		       OR (
		           COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) = $3
		           AND s.video_id < $4
		       ))
		ORDER BY COALESCE(s.ended_at, s.started_at, s.scheduled_start_time, s.last_seen_at) DESC,
		         s.video_id DESC
		LIMIT $5`,
		channelID,
		since,
		cursorAt,
		cursorVideoID,
		broadcastHistoryPageSize,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("query broadcast history: %w", err)
	}
	defer rows.Close()

	entries := make([]handlercore.BroadcastHistoryEntry, 0, broadcastHistoryPageSize)
	var nextCursorAt *time.Time
	nextCursorVideoID := ""
	for rows.Next() {
		entry, err := scanBroadcastHistoryRow(rows)
		if err != nil {
			return nil, nil, "", err
		}
		sortAt := broadcastHistorySortTime(&entry)
		nextCursorAt = &sortAt
		nextCursorVideoID = entry.VideoID
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", fmt.Errorf("iterate broadcast history rows: %w", err)
	}

	return entries, nextCursorAt, nextCursorVideoID, nil
}

func (r *pgBroadcastHistoryRepository) GetEndedBroadcast(ctx context.Context, query handlercore.BroadcastThumbnailQuery) (*handlercore.BroadcastHistoryEntry, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("broadcast history repository not configured")
	}
	if query.VideoID == "" {
		return nil, nil
	}

	row := r.pool.QueryRow(ctx, broadcastHistorySelectSQL+`
		WHERE s.status = 'ENDED'
		  AND s.video_id = $1
		LIMIT 1`,
		query.VideoID,
	)

	entry, err := scanBroadcastHistoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

const broadcastHistorySelectSQL = `
	SELECT s.video_id,
		s.channel_id,
		COALESCE(NULLIF(m.short_korean_name, ''), NULLIF(m.korean_name, ''), NULLIF(m.english_name, ''), s.channel_id) AS member_name,
		COALESCE(s.title, '') AS title,
		COALESCE(NULLIF(s.topic_id, ''), NULLIF(e.topic_id, ''), '') AS topic_id,
		COALESCE(NULLIF(s.thumbnail_url, ''), NULLIF(e.thumbnail_url, ''), '') AS thumbnail_url,
		s.scheduled_start_time,
		s.started_at,
		s.ended_at,
		s.last_seen_at
	FROM youtube_live_sessions s
	LEFT JOIN members m ON m.channel_id = s.channel_id
	LEFT JOIN LATERAL (
		SELECT payload #>> '{notification,stream,topic_id}' AS topic_id,
		       payload #>> '{notification,stream,thumbnail}' AS thumbnail_url
		FROM alarm_dispatch_events e
		WHERE e.alarm_type = 'LIVE'
		  AND e.stream_id = s.video_id
		ORDER BY e.created_at DESC
		LIMIT 1
	) e ON TRUE`

type broadcastHistoryScanner interface {
	Scan(dest ...any) error
}

func scanBroadcastHistoryRow(row broadcastHistoryScanner) (handlercore.BroadcastHistoryEntry, error) {
	var entry handlercore.BroadcastHistoryEntry
	if err := row.Scan(
		&entry.VideoID,
		&entry.ChannelID,
		&entry.MemberName,
		&entry.Title,
		&entry.TopicID,
		&entry.ThumbnailURL,
		&entry.ScheduledStartTime,
		&entry.StartedAt,
		&entry.EndedAt,
		&entry.LastSeenAt,
	); err != nil {
		return handlercore.BroadcastHistoryEntry{}, fmt.Errorf("scan broadcast history row: %w", err)
	}
	classification := ClassifyBroadcastWithSource(entry.TopicID, entry.Title)
	entry.BroadcastType = string(classification.Type)
	entry.BroadcastTypeSource = classification.Source
	return entry, nil
}

func broadcastHistoryEntryMatches(query *handlercore.BroadcastHistoryQuery, entry *handlercore.BroadcastHistoryEntry) bool {
	if query.TopicID != "" && !broadcastTopicMatches(entry.TopicID, query.TopicID) {
		return false
	}
	if query.Type != "" && entry.BroadcastType != query.Type {
		return false
	}
	return true
}

func broadcastHistorySortTime(entry *handlercore.BroadcastHistoryEntry) time.Time {
	switch {
	case entry.EndedAt != nil:
		return entry.EndedAt.UTC()
	case entry.StartedAt != nil:
		return entry.StartedAt.UTC()
	case entry.ScheduledStartTime != nil:
		return entry.ScheduledStartTime.UTC()
	default:
		return entry.LastSeenAt.UTC()
	}
}

func normalizeBroadcastHistoryLimit(limit int) int {
	if limit <= 0 {
		return defaultBroadcastHistoryLimit
	}
	if limit > maxBroadcastHistoryLimit {
		return maxBroadcastHistoryLimit
	}
	return limit
}
