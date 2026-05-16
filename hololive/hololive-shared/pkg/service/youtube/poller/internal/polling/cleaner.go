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
	"log/slog"
	"time"

	"gorm.io/gorm"

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
	db  *gorm.DB
	cfg ViewerSampleCleanerConfig
}

func NewViewerSampleCleaner(db *gorm.DB, cfg ViewerSampleCleanerConfig) *ViewerSampleCleaner {
	return &ViewerSampleCleaner{
		db:  db,
		cfg: cfg,
	}
}

func (c *ViewerSampleCleaner) Cleanup(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -c.cfg.RetentionDays)

	result := c.db.WithContext(ctx).
		Where("video_id IN (SELECT video_id FROM youtube_live_sessions WHERE status = ? AND ended_at < ?)",
			domain.LiveStatusEnded, cutoff).
		Delete(&domain.YouTubeLiveViewerSample{})

	if result.Error != nil {
		return 0, result.Error
	}

	if result.RowsAffected > 0 {
		slog.Info("Cleaned up old viewer samples",
			"deleted", result.RowsAffected,
			"retention_days", c.cfg.RetentionDays)
	}

	return result.RowsAffected, nil
}

func (c *ViewerSampleCleaner) CleanupOldSessions(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	result := c.db.WithContext(ctx).
		Where("status = ? AND ended_at < ?", domain.LiveStatusEnded, cutoff).
		Delete(&domain.YouTubeLiveSession{})

	if result.Error != nil {
		return 0, result.Error
	}

	if result.RowsAffected > 0 {
		slog.Info("Cleaned up old live sessions",
			"deleted", result.RowsAffected,
			"retention_days", retentionDays)
	}

	return result.RowsAffected, nil
}
