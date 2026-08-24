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
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

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
