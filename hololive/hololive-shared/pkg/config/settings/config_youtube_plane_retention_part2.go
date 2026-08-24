package settings

import (
	"errors"
	"fmt"
	"time"
)

func maxEvidenceRetentionAge(cfg *YouTubePlaneRetentionConfig) time.Duration {
	ages := []time.Duration{
		cfg.CommunityPageAge,
		cfg.VideoListAge,
		cfg.ShortsListAge,
		cfg.LiveSnapshotAge,
		cfg.ViewerSampleAge,
		cfg.ChannelStatsAge,
		cfg.ChannelProfileAge,
		cfg.ChannelPhotoAge,
		cfg.ScheduleSnapshotAge,
	}

	var maxAge time.Duration

	for _, age := range ages {
		if age > maxAge {
			maxAge = age
		}
	}

	return maxAge
}

func validateRetentionAge(name string, age time.Duration) error {
	if age < 0 || age > youtubePlaneRetentionMaxAge {
		return fmt.Errorf("youtube plane %s retention must be between 0 and 3650 days", name)
	}

	return nil
}

func (c *YouTubePlaneConfig) validateProductionRetention(environment string) error {
	if !isProductionEnvironment(environment) || !c.Enabled {
		return nil
	}

	if !c.Retention.Enabled {
		return errors.New("YOUTUBE_PLANE_RETENTION_ENABLED=true is required in production")
	}

	if !c.Retention.PolicyApproved {
		return errors.New("YOUTUBE_PLANE_RETENTION_POLICY_APPROVED=true is required in production")
	}

	ages := []struct {
		key string
		age time.Duration
	}{
		{"YOUTUBE_PLANE_RETENTION_QUEUE_PROCESSED_DAYS", c.Retention.QueueProcessedAge},
		{"YOUTUBE_PLANE_RETENTION_QUEUE_DLQ_DAYS", c.Retention.QueueDLQAge},
		{"YOUTUBE_PLANE_RETENTION_COLLISION_DAYS", c.Retention.CollisionAge},
		{"YOUTUBE_PLANE_RETENTION_REPLAY_AUDIT_DAYS", c.Retention.ReplayAuditAge},
		{"YOUTUBE_PLANE_RETENTION_APPLICATION_AUDIT_GRACE_DAYS", c.Retention.ApplicationAuditGrace},
		{"YOUTUBE_PLANE_RETENTION_CHECKPOINT_HISTORY_DAYS", c.Retention.CheckpointHistoryAge},
		{"YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS", c.Retention.ProjectionRetiredAge},
		{"YOUTUBE_PLANE_RETENTION_COMMUNITY_PAGE_DAYS", c.Retention.CommunityPageAge},
		{"YOUTUBE_PLANE_RETENTION_VIDEO_LIST_DAYS", c.Retention.VideoListAge},
		{"YOUTUBE_PLANE_RETENTION_SHORTS_LIST_DAYS", c.Retention.ShortsListAge},
		{"YOUTUBE_PLANE_RETENTION_LIVE_SNAPSHOT_DAYS", c.Retention.LiveSnapshotAge},
		{"YOUTUBE_PLANE_RETENTION_VIEWER_SAMPLE_DAYS", c.Retention.ViewerSampleAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_STATS_DAYS", c.Retention.ChannelStatsAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PROFILE_DAYS", c.Retention.ChannelProfileAge},
		{"YOUTUBE_PLANE_RETENTION_CHANNEL_PHOTO_DAYS", c.Retention.ChannelPhotoAge},
		{"YOUTUBE_PLANE_RETENTION_SCHEDULE_SNAPSHOT_DAYS", c.Retention.ScheduleSnapshotAge},
	}
	for _, item := range ages {
		if item.age <= 0 {
			return fmt.Errorf("%s must be positive in production", item.key)
		}
	}

	return nil
}
