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

package server

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

// GetMilestones: 달성된 마일스톤 목록을 반환합니다.
// GET /api/milestones?limit=50&offset=0&channelId=xxx&memberName=xxx.
func (h *MilestoneAPIHandler) GetMilestones(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	// 기본값
	limit := 50
	offset := 0

	if l := c.Query("limit"); l != "" {
		parsed, err := parseInt(l)
		if err != nil || parsed <= 0 || parsed > 100 {
			c.JSON(400, gin.H{"error": "limit must be an integer between 1 and 100"})
			return
		}

		limit = parsed
	}

	if o := c.Query("offset"); o != "" {
		parsed, err := parseInt(o)
		if err != nil || parsed < 0 {
			c.JSON(400, gin.H{"error": "offset must be an integer greater than or equal to 0"})
			return
		}

		offset = parsed
	}

	filter := stats.MilestoneFilter{
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
// 기본 threshold: 백그라운드 워커와 동일한 95% (MilestoneThresholdRatio).
func (h *MilestoneAPIHandler) GetNearMilestoneMembers(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	// 기본값: 백그라운드 워커와 동일한 95%
	threshold := youtube.MilestoneThresholdRatio

	if t := c.Query("threshold"); t != "" {
		parsed, err := parseFloat(t)
		if err != nil || parsed <= 0 || parsed >= 1 {
			c.JSON(400, gin.H{"error": "threshold must be a number between 0 and 1"})
			return
		}

		threshold = parsed
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

	c.JSON(200, gin.H{
		"status":    "ok",
		"members":   members,
		"count":     len(members),
		"threshold": threshold,
	})
}

// GetMilestoneStats: 마일스톤 관련 통계 요약을 반환합니다.
// GET /api/milestones/stats.
func (h *MilestoneAPIHandler) GetMilestoneStats(c *gin.Context) {
	ctx := c.Request.Context()

	if h.statsRepo == nil {
		c.JSON(503, gin.H{"error": "Stats repository not available"})
		return
	}

	summary, err := h.statsRepo.GetMilestoneStats(ctx)
	if err != nil {
		h.logger.Error("Failed to get milestone stats", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get milestone stats"})

		return
	}

	// 직전 멤버 수 조회 (95% 이상)
	nearCount, err := h.statsRepo.CountNearMilestoneMembers(ctx, youtube.MilestoneThresholdRatio, youtube.SubscriberMilestones)
	if err != nil {
		h.logger.Error("Failed to get near milestone summary", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get near milestone summary"})

		return
	}

	summary.TotalNearMilestone = nearCount

	c.JSON(200, gin.H{
		"status": "ok",
		"stats":  summary,
	})
}

// parseInt: 문자열을 정수로 파싱.
func parseInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("parseInt: %w", err)
	}

	return n, nil
}

// parseFloat: 문자열을 실수로 파싱.
func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parseFloat: %w", err)
	}

	return f, nil
}
