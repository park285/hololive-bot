package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type pgBroadcastHistoryRepository struct {
	pool broadcastHistoryDB
}

const (
	defaultBroadcastHistoryLimit     = 8
	maxBroadcastHistoryLimit         = 20
	maxBroadcastHistoryQueryLimit    = 50
	broadcastHistoryPageSize         = 100
	maxBroadcastHistoryPages         = 5
	maxBroadcastHistoryProcessedRows = broadcastHistoryPageSize * maxBroadcastHistoryPages
	maxBroadcastHistoryScannedRows   = (broadcastHistoryPageSize + 1) * maxBroadcastHistoryPages
	broadcastHistoryQueryBudget      = 2 * time.Second
)

type broadcastHistoryCollector struct {
	query         *handlercore.BroadcastHistoryQuery
	since         *time.Time
	limit         int
	pageSize      int
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

func NewPgBroadcastHistoryRepository(pool *pgxpool.Pool) handlercore.BroadcastHistoryRepository {
	return &pgBroadcastHistoryRepository{pool: pool}
}

func (r *pgBroadcastHistoryRepository) ListEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery) (handlercore.BroadcastHistoryResult, error) {
	if query == nil {
		return handlercore.BroadcastHistoryResult{}, errors.New("broadcast history query is required")
	}

	limit := min(query.Limit, maxBroadcastHistoryQueryLimit)
	if limit <= 0 {
		limit = defaultBroadcastHistoryLimit
	}

	since := broadcastHistorySinceCursor(query)
	queryCtx, cancel := context.WithTimeout(ctx, broadcastHistoryQueryBudget)

	defer cancel()

	result, err := r.collectEndedBroadcasts(queryCtx, query, since, limit)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			result.Truncated = true
			return result, nil
		}

		return handlercore.BroadcastHistoryResult{}, fmt.Errorf("collect ended broadcasts: %w", err)
	}

	return result, nil
}

func (r *pgBroadcastHistoryRepository) collectEndedBroadcasts(ctx context.Context, query *handlercore.BroadcastHistoryQuery, since *time.Time, limit int) (handlercore.BroadcastHistoryResult, error) {
	collector := newBroadcastHistoryCollector(query, since, limit)
	for len(collector.result.Entries) < limit {
		done, err := collector.collectNextPage(ctx, r)
		if err != nil {
			return collector.result, fmt.Errorf("collect next page: %w", err)
		}

		if done {
			break
		}
	}

	return collector.result, nil
}

func newBroadcastHistoryCollector(query *handlercore.BroadcastHistoryQuery, since *time.Time, limit int) *broadcastHistoryCollector {
	return &broadcastHistoryCollector{
		query:    query,
		since:    since,
		limit:    limit,
		pageSize: broadcastHistoryPageSizeFor(query, limit),
		result:   handlercore.BroadcastHistoryResult{Entries: make([]handlercore.BroadcastHistoryEntry, 0, limit)},
	}
}

func broadcastHistoryPageSizeFor(query *handlercore.BroadcastHistoryQuery, limit int) int {
	if query.Type == "" && query.TopicID == "" {
		return limit
	}

	return broadcastHistoryPageSize
}

func (c *broadcastHistoryCollector) collectNextPage(ctx context.Context, repository *pgBroadcastHistoryRepository) (bool, error) {
	page, err := repository.listEndedBroadcastPage(ctx, c.query, c.since, c.cursorAt, c.cursorVideoID, c.pageSize+1)
	if err != nil {
		return false, fmt.Errorf("list ended broadcast page: %w", err)
	}

	if err := c.recordScannedPage(len(page)); err != nil {
		return false, fmt.Errorf("record scanned page: %w", err)
	}

	if len(page) == 0 {
		return true, nil
	}

	page, hasMore := boundedBroadcastHistoryPage(page, c.pageSize)

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

func boundedBroadcastHistoryPage(page []handlercore.BroadcastHistoryEntry, pageSize int) ([]handlercore.BroadcastHistoryEntry, bool) {
	if len(page) > pageSize {
		return page[:pageSize], true
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
	if query.Since.IsZero() {
		value := time.Now().AddDate(0, 0, -MaxBroadcastHistoryDays).UTC()
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

func (r *pgBroadcastHistoryRepository) listEndedBroadcastPage(
	ctx context.Context,
	query *handlercore.BroadcastHistoryQuery,
	since, cursorAt *time.Time,
	cursorVideoID string,
	pageLimit int,
) ([]handlercore.BroadcastHistoryEntry, error) {
	topicFilter := broadcastHistorySQLTopicFilter(query.TopicID)

	rows, err := r.pool.Query(ctx, broadcastHistoryPageSQL(query.ChannelID),
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
			return nil, fmt.Errorf("scan broadcast history row: %w", err)
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
	if query.VideoID == "" {
		return nil, handlercore.ErrBroadcastNotFound
	}

	row := r.pool.QueryRow(ctx, getEndedBroadcastSQL, query.VideoID)

	entry, err := scanBroadcastHistoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, handlercore.ErrBroadcastNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("scan broadcast history row: %w", err)
	}

	return &entry, nil
}

func broadcastHistoryPageSQL(channelID string) string {
	if channelID == "" {
		return broadcastHistoryAllChannelsPageSQL
	}

	return broadcastHistoryChannelPageSQL
}

type broadcastHistoryScanner interface {
	Scan(dest ...any) error
}

func scanBroadcastHistoryRow(row broadcastHistoryScanner) (handlercore.BroadcastHistoryEntry, error) {
	var (
		entry              handlercore.BroadcastHistoryEntry
		decisionType       *string
		decisionConfidence *float64
		signals            BroadcastTypeSignals
	)

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
		&entry.LifecycleOrigin,
		&entry.LastLivePositiveAt,
		&entry.TimesSource,
		&entry.FinalizationOutcome,
		&entry.FinalizationReason,
		&decisionType,
		&decisionConfidence,
		&signals.PlayerMembersOnly,
	); err != nil {
		return handlercore.BroadcastHistoryEntry{}, fmt.Errorf("scan broadcast history row: %w", err)
	}

	if decisionType != nil && decisionConfidence != nil {
		signals.Decision = BroadcastTypeDecision{Type: broadcasttype.Type(*decisionType), Confidence: *decisionConfidence}
	}

	classification := ClassifyBroadcastVideo(entry.VideoID, entry.ChannelID, entry.TopicID, entry.Title, signals)

	entry.BroadcastType = string(classification.Type)

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
