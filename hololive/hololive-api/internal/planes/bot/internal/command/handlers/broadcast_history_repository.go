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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type pgBroadcastHistoryRepository struct {
	pool         broadcastHistoryDB
	queryTimeout time.Duration
	pageLoader   broadcastHistoryPageLoader
}

const (
	defaultBroadcastHistoryLimit       = 8
	maxBroadcastHistoryLimit           = 20
	broadcastHistoryPageSize           = 100
	maxBroadcastHistoryPages           = 5
	maxBroadcastHistoryProcessedRows   = broadcastHistoryPageSize * maxBroadcastHistoryPages
	maxBroadcastHistoryScannedRows     = (broadcastHistoryPageSize + 1) * maxBroadcastHistoryPages
	defaultBroadcastHistoryQueryBudget = 2 * time.Second
)

type broadcastHistoryPageLoader func(
	ctx context.Context,
	query *handlercore.BroadcastHistoryQuery,
	since *time.Time,
	cursorAt *time.Time,
	cursorVideoID string,
	pageLimit int,
) ([]handlercore.BroadcastHistoryEntry, error)

type broadcastHistoryCollector struct {
	query         *handlercore.BroadcastHistoryQuery
	since         *time.Time
	limit         int
	result        handlercore.BroadcastHistoryResult
	cursorAt      *time.Time
	cursorVideoID string
	pages         int
	scannedRows   int
}

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

func (r *pgBroadcastHistoryRepository) ListEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery) (handlercore.BroadcastHistoryResult, error) {
	if r == nil || (r.pool == nil && r.pageLoader == nil) {
		return handlercore.BroadcastHistoryResult{}, errors.New("broadcast history repository not configured")
	}
	if query == nil {
		return handlercore.BroadcastHistoryResult{}, errors.New("broadcast history query is required")
	}

	limit := normalizeBroadcastHistoryLimit(query.Limit)
	since := broadcastHistorySinceCursor(query)
	queryCtx, cancel := context.WithTimeout(ctx, r.elapsedQueryBudget())
	defer cancel()

	result, err := r.collectEndedBroadcasts(queryCtx, query, since, limit)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			result.Truncated = true
			return result, nil
		}
		return handlercore.BroadcastHistoryResult{}, err
	}
	return result, nil
}

func (r *pgBroadcastHistoryRepository) elapsedQueryBudget() time.Duration {
	if r.queryTimeout > 0 {
		return r.queryTimeout
	}
	return defaultBroadcastHistoryQueryBudget
}

func (r *pgBroadcastHistoryRepository) collectEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery, since *time.Time, limit int) (handlercore.BroadcastHistoryResult, error) {
	collector := newBroadcastHistoryCollector(query, since, limit)
	for len(collector.result.Entries) < limit {
		done, err := collector.collectNextPage(ctx, r)
		if err != nil {
			return collector.result, err
		}
		if done {
			break
		}
	}
	return collector.result, nil
}

func newBroadcastHistoryCollector(query *handlercore.BroadcastHistoryQuery, since *time.Time, limit int) *broadcastHistoryCollector {
	return &broadcastHistoryCollector{
		query:  query,
		since:  since,
		limit:  limit,
		result: handlercore.BroadcastHistoryResult{Entries: make([]handlercore.BroadcastHistoryEntry, 0, limit)},
	}
}

func (c *broadcastHistoryCollector) collectNextPage(ctx context.Context, repository *pgBroadcastHistoryRepository) (bool, error) {
	page, err := repository.loadEndedBroadcastPage(ctx, c.query, c.since, c.cursorAt, c.cursorVideoID, broadcastHistoryPageSize+1)
	if err != nil {
		return false, err
	}
	if err := c.recordScannedPage(len(page)); err != nil {
		return false, err
	}
	if len(page) == 0 {
		return true, nil
	}

	page, hasMore := boundedBroadcastHistoryPage(page)
	c.result.Entries = appendMatchingBroadcastHistoryEntries(c.result.Entries, page, c.query, c.limit)
	if len(c.result.Entries) >= c.limit || !hasMore {
		return true, nil
	}
	if c.pages >= maxBroadcastHistoryPages || c.scannedRows >= maxBroadcastHistoryScannedRows {
		c.result.Truncated = true
		return true, nil
	}
	c.advanceCursor(page)
	return false, nil
}

func (c *broadcastHistoryCollector) recordScannedPage(rows int) error {
	c.pages++
	c.scannedRows += rows
	if c.scannedRows > maxBroadcastHistoryScannedRows {
		return fmt.Errorf("broadcast history scan budget exceeded: %d rows", c.scannedRows)
	}
	return nil
}

func boundedBroadcastHistoryPage(page []handlercore.BroadcastHistoryEntry) ([]handlercore.BroadcastHistoryEntry, bool) {
	hasMore := len(page) > broadcastHistoryPageSize
	if hasMore {
		return page[:broadcastHistoryPageSize], true
	}
	return page, false
}

func (c *broadcastHistoryCollector) advanceCursor(page []handlercore.BroadcastHistoryEntry) {
	last := &page[len(page)-1]
	nextCursorAt := broadcastHistorySortTime(last)
	c.cursorAt = &nextCursorAt
	c.cursorVideoID = last.VideoID
}

func broadcastHistorySinceCursor(query *handlercore.BroadcastHistoryQuery) *time.Time {
	if query.IncludeAll || query.Since.IsZero() {
		value := time.Now().AddDate(0, 0, -maxBroadcastHistoryDays).UTC()
		return &value
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

func (r *pgBroadcastHistoryRepository) loadEndedBroadcastPage(
	ctx context.Context,
	query *handlercore.BroadcastHistoryQuery,
	since, cursorAt *time.Time,
	cursorVideoID string,
	pageLimit int,
) ([]handlercore.BroadcastHistoryEntry, error) {
	if r.pageLoader != nil {
		return r.pageLoader(ctx, query, since, cursorAt, cursorVideoID, pageLimit)
	}
	return r.listEndedBroadcastPage(ctx, query, since, cursorAt, cursorVideoID, pageLimit)
}

func (r *pgBroadcastHistoryRepository) listEndedBroadcastPage(
	ctx context.Context,
	query *handlercore.BroadcastHistoryQuery,
	since, cursorAt *time.Time,
	cursorVideoID string,
	pageLimit int,
) ([]handlercore.BroadcastHistoryEntry, error) {
	topicFilter := broadcastHistorySQLTopicFilter(query.TopicID)
	rows, err := r.pool.Query(ctx, broadcastHistoryListPageSQL,
		query.ChannelID,
		since,
		cursorAt,
		cursorVideoID,
		topicFilter,
		pageLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("query broadcast history: %w", err)
	}
	defer rows.Close()

	entries := make([]handlercore.BroadcastHistoryEntry, 0, pageLimit)
	for rows.Next() {
		entry, err := scanBroadcastHistoryRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate broadcast history rows: %w", err)
	}

	return entries, nil
}

func broadcastHistorySQLTopicFilter(topicID string) string {
	normalized := normalizeBroadcastTopic(topicID)
	if normalized == "" || strings.Contains(normalized, ",") {
		return ""
	}
	for _, r := range normalized {
		if r > 127 {
			return ""
		}
	}
	return normalized
}

func (r *pgBroadcastHistoryRepository) GetEndedBroadcast(ctx context.Context, query handlercore.BroadcastThumbnailQuery) (*handlercore.BroadcastHistoryEntry, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("broadcast history repository not configured")
	}
	if query.VideoID == "" {
		return nil, nil
	}

	row := r.pool.QueryRow(ctx, broadcastHistoryGetByVideoIDSQL, query.VideoID)

	entry, err := scanBroadcastHistoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

var (
	broadcastHistoryListPageSQL     = mustSQL("broadcast_history_repository_0179_01.sql")
	broadcastHistoryGetByVideoIDSQL = mustSQL("broadcast_history_repository_0242_02.sql")
)

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
