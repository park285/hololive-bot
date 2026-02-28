package server

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

// GetMilestones: 달성된 마일스톤 목록을 반환합니다.
// GET /api/milestones?limit=50&offset=0&channelId=xxx&memberName=xxx
func (h *APIHandler) GetMilestones(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	// 기본값
	limit := 50
	offset := 0

	if l := c.Query("limit"); l != "" {
		if parsed, err := parseInt(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := parseInt(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	filter := youtube.MilestoneFilter{
		Limit:      limit,
		Offset:     offset,
		ChannelID:  c.Query("channelId"),
		MemberName: c.Query("memberName"),
	}

	result, err := h.statsRepo.GetAllMilestones(ctx, filter)
	if err != nil {
		h.logger.Error("Failed to get milestones", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get milestones"})
		return
	}

	c.JSON(200, gin.H{
		"status":     "ok",
		"milestones": result.Milestones,
		"total":      result.Total,
		"limit":      result.Limit,
		"offset":     result.Offset,
	})
}

// GetNearMilestoneMembers: 마일스톤 달성 직전의 멤버 목록을 반환합니다.
// GET /api/milestones/near?threshold=0.9
// 기본 threshold: 백그라운드 워커와 동일한 95% (MilestoneThresholdRatio)
func (h *APIHandler) GetNearMilestoneMembers(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	// 기본값: 백그라운드 워커와 동일한 95%
	threshold := youtube.MilestoneThresholdRatio
	if t := c.Query("threshold"); t != "" {
		if parsed, err := parseFloat(t); err == nil && parsed > 0 && parsed < 1 {
			threshold = parsed
		}
	}

	// 항상 6명만 조회 (졸업 멤버 제외는 Repo 내부 JOIN으로 자동 처리됨)
	limit := 6

	members, err := h.statsRepo.GetNearMilestoneMembers(ctx, threshold, youtube.SubscriberMilestones, limit)
	if err != nil {
		h.logger.Error("Failed to get near milestone members", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get near milestone members"})
		return
	}

	// 안전장치: DB Limit 외에도 한 번 더 자름
	if len(members) > limit {
		members = members[:limit]
	}

	// 임박 멤버(threshold 이상)가 없으면, 기준을 없애고(threshold=0) Top 4를 다시 조회
	if len(members) == 0 {
		closest, err := h.statsRepo.GetClosestMilestoneMembers(ctx, limit, youtube.SubscriberMilestones)
		if err == nil && len(closest) > 0 {
			members = closest
			threshold = 0 // UI에서 배지 숨김 처리
		} else if err != nil {
			h.logger.Warn("Failed to get closest milestone members fallback", slog.Any("error", err))
		}
	}

	c.JSON(200, gin.H{
		"status":    "ok",
		"members":   members,
		"count":     len(members),
		"threshold": threshold,
	})
}

// GetMilestoneStats: 마일스톤 관련 통계 요약을 반환합니다.
// GET /api/milestones/stats
func (h *APIHandler) GetMilestoneStats(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	stats, err := h.statsRepo.GetMilestoneStats(ctx)
	if err != nil {
		h.logger.Error("Failed to get milestone stats", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get milestone stats"})
		return
	}

	// 직전 멤버 수도 함께 조회 (95% 이상)
	// 직전 멤버 수도 함께 조회 (95% 이상)
	nearMembers, err := h.statsRepo.GetNearMilestoneMembers(ctx, youtube.MilestoneThresholdRatio, youtube.SubscriberMilestones, 50)
	if err == nil {
		stats.TotalNearMilestone = len(nearMembers)
	}

	c.JSON(200, gin.H{
		"status": "ok",
		"stats":  stats,
	})
}

// parseInt: 문자열을 정수로 파싱
func parseInt(s string) (int, error) {
	var i int
	_, err := parseWithFormat(s, "%d", &i)
	return i, err
}

// parseFloat: 문자열을 실수로 파싱
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := parseWithFormat(s, "%f", &f)
	return f, err
}

// parseWithFormat: fmt.Sscanf 래퍼
func parseWithFormat(s, format string, a any) (int, error) {
	n, err := fmt.Sscanf(s, format, a)
	if err != nil {
		return n, fmt.Errorf("parse format error: %w", err)
	}
	return n, nil
}
