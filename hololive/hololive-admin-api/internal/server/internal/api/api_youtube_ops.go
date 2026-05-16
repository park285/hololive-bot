package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

const (
	youtubeCommunityShortsOpsWindowHours           = 24
	youtubeCommunityShortsSLAThresholdMillis int64 = 120000
)

var youtubeCommunityShortsOpsWindow = 24 * time.Hour

type YouTubeCommunityShortsOpsRepository interface {
	ListPostSendCountsSince(ctx context.Context, since time.Time) ([]outbox.PostSendCount, error)
}

type YouTubeCommunityShortsOpsResponse struct {
	Status             string                             `json:"status"`
	GeneratedAt        time.Time                          `json:"generatedAt"`
	WindowStart        time.Time                          `json:"windowStart"`
	WindowEnd          time.Time                          `json:"windowEnd"`
	WindowHours        int                                `json:"windowHours"`
	ObservedAtBasis    string                             `json:"observedAtBasis"`
	SlaThresholdMillis int64                              `json:"slaThresholdMillis"`
	Overview           YouTubeCommunityShortsOpsOverview  `json:"overview"`
	Channels           []YouTubeCommunityShortsOpsChannel `json:"channels"`
}

type YouTubeCommunityShortsOpsOverview struct {
	ChannelCount               int64  `json:"channelCount"`
	DetectedPostCount          int64  `json:"detectedPostCount"`
	AlarmSentPostCount         int64  `json:"alarmSentPostCount"`
	SuccessPostCount           int64  `json:"successPostCount"`
	FailedPostCount            int64  `json:"failedPostCount"`
	DetectedUnsentPostCount    int64  `json:"detectedUnsentPostCount"`
	PendingPostCount           int64  `json:"pendingPostCount"`
	LatencyMeasuredPostCount   int64  `json:"latencyMeasuredPostCount"`
	WithinTargetPostCount      int64  `json:"withinTargetPostCount"`
	ExceededPostCount          int64  `json:"exceededPostCount"`
	CommunityDetectedPostCount int64  `json:"communityDetectedPostCount"`
	ShortsDetectedPostCount    int64  `json:"shortsDetectedPostCount"`
	CommunityExceededPostCount int64  `json:"communityExceededPostCount"`
	ShortsExceededPostCount    int64  `json:"shortsExceededPostCount"`
	AverageLatencyMillis       *int64 `json:"averageLatencyMillis,omitempty"`
	MaxLatencyMillis           *int64 `json:"maxLatencyMillis,omitempty"`
}

type YouTubeCommunityShortsOpsChannel struct {
	ChannelID                string     `json:"channelId"`
	MemberName               string     `json:"memberName,omitempty"`
	EarliestObservedAt       *time.Time `json:"earliestObservedAt,omitempty"`
	LatestObservedAt         *time.Time `json:"latestObservedAt,omitempty"`
	DetectedPostCount        int64      `json:"detectedPostCount"`
	AlarmSentPostCount       int64      `json:"alarmSentPostCount"`
	SuccessPostCount         int64      `json:"successPostCount"`
	FailedPostCount          int64      `json:"failedPostCount"`
	DetectedUnsentPostCount  int64      `json:"detectedUnsentPostCount"`
	PendingPostCount         int64      `json:"pendingPostCount"`
	LatencyMeasuredPostCount int64      `json:"latencyMeasuredPostCount"`
	WithinTargetPostCount    int64      `json:"withinTargetPostCount"`
	ExceededPostCount        int64      `json:"exceededPostCount"`
	CommunityPostCount       int64      `json:"communityPostCount"`
	ShortsPostCount          int64      `json:"shortsPostCount"`
	AverageLatencyMillis     *int64     `json:"averageLatencyMillis,omitempty"`
	MaxLatencyMillis         *int64     `json:"maxLatencyMillis,omitempty"`
}

type youtubeCommunityShortsChannelLatencySummary struct {
	PendingPostCount         int64
	LatencyMeasuredPostCount int64
	WithinTargetPostCount    int64
	ExceededPostCount        int64
	AverageLatencyMillis     *int64
	MaxLatencyMillis         *int64
}

type youtubeCommunityShortsChannelLatencyAccumulator struct {
	summary              youtubeCommunityShortsChannelLatencySummary
	latencySumMillis     int64
	latencyMeasuredCount int64
	maxLatencyMillis     int64
}

func (h *StatsAPIHandler) GetYouTubeCommunityShortsOps(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if h == nil || h.APIHandler == nil || h.communityShortsOps == nil {
		sharedserver.RespondError(c, 503, "YouTube community/shorts ops repository not available", nil)
		return
	}

	now := time.Now().UTC()
	windowStart := now.Add(-youtubeCommunityShortsOpsWindow)

	posts, err := h.communityShortsOps.ListPostSendCountsSince(ctx, windowStart)
	if err != nil {
		h.safeLogger().Error("Failed to load YouTube community/shorts ops posts", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to load YouTube community/shorts ops posts", nil)
		return
	}

	channelSummaries, err := outbox.BuildChannelPostDeliverySummaries(posts)
	if err != nil {
		h.safeLogger().Error("Failed to build YouTube community/shorts channel summaries", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to build YouTube community/shorts channel summaries", nil)
		return
	}

	latencySummaries, err := outbox.BuildPostLatencyPeriodSummaries(
		posts,
		youtubeCommunityShortsOpsLatencyPeriods(windowStart, now),
	)
	if err != nil {
		h.safeLogger().Error("Failed to build YouTube community/shorts latency summaries", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to build YouTube community/shorts latency summaries", nil)
		return
	}

	channelLatencySummaries, err := buildYouTubeCommunityShortsChannelLatencySummaries(posts)
	if err != nil {
		h.safeLogger().Error("Failed to build YouTube community/shorts channel latency summaries", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to build YouTube community/shorts channel latency summaries", nil)
		return
	}

	memberNames := h.loadYouTubeCommunityShortsMemberNames(ctx)
	latencySummary := firstYouTubeCommunityShortsOpsLatencySummary(latencySummaries)

	c.JSON(200, YouTubeCommunityShortsOpsResponse{
		Status:             "ok",
		GeneratedAt:        now,
		WindowStart:        windowStart,
		WindowEnd:          now,
		WindowHours:        youtubeCommunityShortsOpsWindowHours,
		ObservedAtBasis:    "COALESCE(actual_published_at, detected_at)",
		SlaThresholdMillis: youtubeCommunityShortsSLAThresholdMillis,
		Overview:           buildYouTubeCommunityShortsOpsOverview(channelSummaries, latencySummary),
		Channels:           buildYouTubeCommunityShortsOpsChannels(channelSummaries, channelLatencySummaries, memberNames),
	})
}

func (h *StatsAPIHandler) loadYouTubeCommunityShortsMemberNames(ctx context.Context) map[string]string {
	memberNames := map[string]string{}
	if h == nil || h.APIHandler == nil || h.memberIndexLoader == nil {
		return memberNames
	}

	members, err := h.memberIndexLoader(ctx)
	if err != nil {
		h.safeLogger().Warn("Failed to load member index for YouTube community/shorts ops", slog.Any("error", err))
		return memberNames
	}

	for i := range members {
		addYouTubeCommunityShortsMemberName(memberNames, members[i])
	}

	return memberNames
}

func addYouTubeCommunityShortsMemberName(memberNames map[string]string, member *domain.Member) {
	if member == nil {
		return
	}
	channelID := strings.TrimSpace(member.ChannelID)
	if channelID == "" {
		return
	}
	memberNames[channelID] = youtubeCommunityShortsMemberName(member, channelID)
}

func youtubeCommunityShortsMemberName(member *domain.Member, channelID string) string {
	memberName := strings.TrimSpace(member.Name)
	if memberName == "" {
		return channelID
	}
	return memberName
}

func youtubeCommunityShortsOpsLatencyPeriods(windowStart, now time.Time) []outbox.PostLatencyPeriod {
	return []outbox.PostLatencyPeriod{{
		Label:   "last_24h",
		StartAt: windowStart,
		EndAt:   now,
	}}
}

func firstYouTubeCommunityShortsOpsLatencySummary(
	latencySummaries []outbox.PostLatencyPeriodSummary,
) outbox.PostLatencyPeriodSummary {
	if len(latencySummaries) == 0 {
		return outbox.PostLatencyPeriodSummary{}
	}
	return latencySummaries[0]
}

func buildYouTubeCommunityShortsOpsOverview(
	channelSummaries []outbox.ChannelPostDeliverySummary,
	latencySummary outbox.PostLatencyPeriodSummary,
) YouTubeCommunityShortsOpsOverview {
	overview := YouTubeCommunityShortsOpsOverview{
		ChannelCount:               int64(len(channelSummaries)),
		PendingPostCount:           latencySummary.PendingPostCount,
		LatencyMeasuredPostCount:   latencySummary.LatencyMeasuredPostCount,
		WithinTargetPostCount:      latencySummary.WithinTargetPostCount,
		ExceededPostCount:          latencySummary.ExceededPostCount,
		CommunityExceededPostCount: latencySummary.CommunityExceededPostCount,
		ShortsExceededPostCount:    latencySummary.ShortsExceededPostCount,
		AverageLatencyMillis:       latencySummary.AverageLatencyMillis,
		MaxLatencyMillis:           latencySummary.MaxLatencyMillis,
	}

	for i := range channelSummaries {
		overview.DetectedPostCount += channelSummaries[i].DetectedPostCount
		overview.AlarmSentPostCount += channelSummaries[i].AlarmSentPostCount
		overview.SuccessPostCount += channelSummaries[i].SuccessPostCount
		overview.FailedPostCount += channelSummaries[i].FailedPostCount
		overview.DetectedUnsentPostCount += channelSummaries[i].DetectedUnsentPostCount
		overview.CommunityDetectedPostCount += channelSummaries[i].CommunityDetectedPostCount
		overview.ShortsDetectedPostCount += channelSummaries[i].ShortsDetectedPostCount
	}

	return overview
}

func buildYouTubeCommunityShortsOpsChannels(
	channelSummaries []outbox.ChannelPostDeliverySummary,
	latencySummaries map[string]youtubeCommunityShortsChannelLatencySummary,
	memberNames map[string]string,
) []YouTubeCommunityShortsOpsChannel {
	rows := make([]YouTubeCommunityShortsOpsChannel, 0, len(channelSummaries))
	for i := range channelSummaries {
		channelID := strings.TrimSpace(channelSummaries[i].ChannelID)
		latencySummary := latencySummaries[channelID]
		rows = append(rows, YouTubeCommunityShortsOpsChannel{
			ChannelID:                channelID,
			MemberName:               memberNames[channelID],
			EarliestObservedAt:       channelSummaries[i].EarliestObservedAt,
			LatestObservedAt:         channelSummaries[i].LatestObservedAt,
			DetectedPostCount:        channelSummaries[i].DetectedPostCount,
			AlarmSentPostCount:       channelSummaries[i].AlarmSentPostCount,
			SuccessPostCount:         channelSummaries[i].SuccessPostCount,
			FailedPostCount:          channelSummaries[i].FailedPostCount,
			DetectedUnsentPostCount:  channelSummaries[i].DetectedUnsentPostCount,
			PendingPostCount:         latencySummary.PendingPostCount,
			LatencyMeasuredPostCount: latencySummary.LatencyMeasuredPostCount,
			WithinTargetPostCount:    latencySummary.WithinTargetPostCount,
			ExceededPostCount:        latencySummary.ExceededPostCount,
			CommunityPostCount:       channelSummaries[i].CommunityDetectedPostCount,
			ShortsPostCount:          channelSummaries[i].ShortsDetectedPostCount,
			AverageLatencyMillis:     latencySummary.AverageLatencyMillis,
			MaxLatencyMillis:         latencySummary.MaxLatencyMillis,
		})
	}
	return rows
}

func buildYouTubeCommunityShortsChannelLatencySummaries(
	posts []outbox.PostSendCount,
) (map[string]youtubeCommunityShortsChannelLatencySummary, error) {
	if len(posts) == 0 {
		return map[string]youtubeCommunityShortsChannelLatencySummary{}, nil
	}

	accumulators := make(map[string]*youtubeCommunityShortsChannelLatencyAccumulator, len(posts))
	for i := range posts {
		channelID := strings.TrimSpace(posts[i].ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("post[%d] channel id is empty", i)
		}

		accumulator, ok := accumulators[channelID]
		if !ok {
			accumulator = &youtubeCommunityShortsChannelLatencyAccumulator{}
			accumulators[channelID] = accumulator
		}

		accumulator.add(posts[i])
	}

	summaries := make(map[string]youtubeCommunityShortsChannelLatencySummary, len(accumulators))
	for channelID, accumulator := range accumulators {
		summaries[channelID] = accumulator.finalize()
	}
	return summaries, nil
}

func (a *youtubeCommunityShortsChannelLatencyAccumulator) add(post outbox.PostSendCount) {
	if post.AlarmSentAt == nil {
		a.summary.PendingPostCount++
	}

	hasLatencyResult := post.AlarmLatencyMillis != nil || post.AlarmLatencyExceeded != nil
	if hasLatencyResult {
		a.summary.LatencyMeasuredPostCount++
	}

	if post.AlarmLatencyExceeded != nil {
		a.addLatencyExceeded(*post.AlarmLatencyExceeded)
	}

	if post.AlarmLatencyMillis != nil {
		a.addLatencyMillis(*post.AlarmLatencyMillis)
	}
}

func (a *youtubeCommunityShortsChannelLatencyAccumulator) addLatencyExceeded(exceeded bool) {
	if exceeded {
		a.summary.ExceededPostCount++
		return
	}
	a.summary.WithinTargetPostCount++
}

func (a *youtubeCommunityShortsChannelLatencyAccumulator) addLatencyMillis(latencyMillis int64) {
	a.latencySumMillis += latencyMillis
	a.latencyMeasuredCount++
	if a.latencyMeasuredCount == 1 || latencyMillis > a.maxLatencyMillis {
		a.maxLatencyMillis = latencyMillis
	}
}

func (a youtubeCommunityShortsChannelLatencyAccumulator) finalize() youtubeCommunityShortsChannelLatencySummary {
	if a.latencyMeasuredCount <= 0 {
		return a.summary
	}

	averageLatencyMillis := a.latencySumMillis / a.latencyMeasuredCount
	maxLatencyMillis := a.maxLatencyMillis
	a.summary.AverageLatencyMillis = &averageLatencyMillis
	a.summary.MaxLatencyMillis = &maxLatencyMillis
	return a.summary
}

var _ YouTubeCommunityShortsOpsRepository = (*outbox.DeliveryTelemetryRepository)(nil)
