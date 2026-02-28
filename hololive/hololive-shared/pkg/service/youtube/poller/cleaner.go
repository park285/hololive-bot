package poller

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
