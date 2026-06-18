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

package polling

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type ViewerSampleCleanerConfig struct {
	RetentionDays int
	BatchSize     int
}

func DefaultViewerSampleCleanerConfig() ViewerSampleCleanerConfig {
	return ViewerSampleCleanerConfig{
		RetentionDays: 7,
		BatchSize:     1000,
	}
}

type ViewerSampleCleaner struct {
	db     dbx.Querier
	config ViewerSampleCleanerConfig
}

func NewViewerSampleCleaner(db any, config ViewerSampleCleanerConfig) *ViewerSampleCleaner {
	return &ViewerSampleCleaner{
		db:     asViewerSampleQuerier(db),
		config: config,
	}
}

func asViewerSampleQuerier(db any) dbx.Querier {
	querier, ok := db.(dbx.Querier)
	if !ok {
		return nil
	}
	return querier
}

func (c *ViewerSampleCleaner) Cleanup(ctx context.Context) (int64, error) {
	if c.db == nil {
		return 0, fmt.Errorf("viewer sample cleaner db is nil")
	}
	cutoff := time.Now().AddDate(0, 0, -c.config.RetentionDays)

	tag, err := c.db.Exec(ctx, `
		DELETE FROM youtube_live_viewer_samples
		WHERE video_id IN (
			SELECT video_id
			FROM youtube_live_sessions
			WHERE status = $1 AND ended_at < $2
		)`,
		domain.LiveStatusEnded,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	rowsAffected := tag.RowsAffected()

	if rowsAffected > 0 {
		slog.Info("Cleaned up old viewer samples",
			"deleted", rowsAffected,
			"retention_days", c.config.RetentionDays)
	}

	return rowsAffected, nil
}
