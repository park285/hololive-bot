package holo

import (
	"context"
)

// YouTubeCommunityShortsOpsOverview는 관리자 커뮤니티·Shorts 관측의 소유 필드와 값의 존재를 보존합니다.
type YouTubeCommunityShortsOpsOverview struct {
	AlarmSentPostCount         *int64 `json:"alarmSentPostCount"`
	AverageLatencyMillis       *int64 `json:"averageLatencyMillis,omitempty"`
	ChannelCount               *int64 `json:"channelCount"`
	CommunityDetectedPostCount *int64 `json:"communityDetectedPostCount"`
	CommunityExceededPostCount *int64 `json:"communityExceededPostCount"`
	DetectedPostCount          *int64 `json:"detectedPostCount"`
	DetectedUnsentPostCount    *int64 `json:"detectedUnsentPostCount"`
	ExceededPostCount          *int64 `json:"exceededPostCount"`
	FailedPostCount            *int64 `json:"failedPostCount"`
	LatencyMeasuredPostCount   *int64 `json:"latencyMeasuredPostCount"`
	MaxLatencyMillis           *int64 `json:"maxLatencyMillis,omitempty"`
	PendingPostCount           *int64 `json:"pendingPostCount"`
	ShortsDetectedPostCount    *int64 `json:"shortsDetectedPostCount"`
	ShortsExceededPostCount    *int64 `json:"shortsExceededPostCount"`
	SuccessPostCount           *int64 `json:"successPostCount"`
	WithinTargetPostCount      *int64 `json:"withinTargetPostCount"`
}

// YouTubeCommunityShortsOpsChannel는 관리자 커뮤니티·Shorts 관측의 소유 필드와 값의 존재를 보존합니다.
type YouTubeCommunityShortsOpsChannel struct {
	AlarmSentPostCount       *int64  `json:"alarmSentPostCount"`
	AverageLatencyMillis     *int64  `json:"averageLatencyMillis,omitempty"`
	ChannelID                *string `json:"channelId"`
	CommunityPostCount       *int64  `json:"communityPostCount"`
	DetectedPostCount        *int64  `json:"detectedPostCount"`
	DetectedUnsentPostCount  *int64  `json:"detectedUnsentPostCount"`
	EarliestObservedAt       *string `json:"earliestObservedAt,omitempty"`
	ExceededPostCount        *int64  `json:"exceededPostCount"`
	FailedPostCount          *int64  `json:"failedPostCount"`
	LatencyMeasuredPostCount *int64  `json:"latencyMeasuredPostCount"`
	LatestObservedAt         *string `json:"latestObservedAt,omitempty"`
	MaxLatencyMillis         *int64  `json:"maxLatencyMillis,omitempty"`
	MemberName               *string `json:"memberName,omitempty"`
	PendingPostCount         *int64  `json:"pendingPostCount"`
	ShortsPostCount          *int64  `json:"shortsPostCount"`
	SuccessPostCount         *int64  `json:"successPostCount"`
	WithinTargetPostCount    *int64  `json:"withinTargetPostCount"`
}

// YouTubeCommunityShortsOpsResponse는 관리자 커뮤니티·Shorts 관측의 소유 필드와 값의 존재를 보존합니다.
type YouTubeCommunityShortsOpsResponse struct {
	Channels           []YouTubeCommunityShortsOpsChannel `json:"channels"`
	GeneratedAt        *string                            `json:"generatedAt"`
	ObservedAtBasis    *string                            `json:"observedAtBasis"`
	Overview           *YouTubeCommunityShortsOpsOverview `json:"overview"`
	SLAThresholdMillis *int64                             `json:"slaThresholdMillis"`
	Status             string                             `json:"status"`
	WindowEnd          *string                            `json:"windowEnd"`
	WindowHours        *int64                             `json:"windowHours"`
	WindowStart        *string                            `json:"windowStart"`
}

func (r YouTubeCommunityShortsOpsOverview) valid() bool {
	return nonnegative(r.ChannelCount, r.DetectedPostCount, r.AlarmSentPostCount, r.SuccessPostCount, r.FailedPostCount, r.DetectedUnsentPostCount, r.PendingPostCount, r.LatencyMeasuredPostCount, r.WithinTargetPostCount, r.ExceededPostCount, r.CommunityDetectedPostCount, r.ShortsDetectedPostCount, r.CommunityExceededPostCount, r.ShortsExceededPostCount)
}

func (r YouTubeCommunityShortsOpsChannel) valid() bool {
	return nonnegative(r.DetectedPostCount, r.AlarmSentPostCount, r.SuccessPostCount, r.FailedPostCount, r.DetectedUnsentPostCount, r.PendingPostCount, r.LatencyMeasuredPostCount, r.WithinTargetPostCount, r.ExceededPostCount, r.CommunityPostCount, r.ShortsPostCount) && r.ChannelID != nil && timestamp(r.EarliestObservedAt) && timestamp(r.LatestObservedAt)
}

func (r YouTubeCommunityShortsOpsResponse) valid() bool {
	if r.Status != "ok" || !present(r.GeneratedAt, r.WindowStart, r.WindowEnd, r.ObservedAtBasis) || !timestamp(r.GeneratedAt) || !timestamp(r.WindowStart) || !timestamp(r.WindowEnd) || !nonnegative(r.WindowHours, r.SLAThresholdMillis) || r.Overview == nil || !r.Overview.valid() || r.Channels == nil {
		return false
	}

	for index := range r.Channels {
		if !r.Channels[index].valid() {
			return false
		}
	}

	return true
}

// GetYouTubeCommunityShortsOps는 화면 밖의 등록된 진단 API도 같은 응답 경계로 조회합니다.
func (c *Client) GetYouTubeCommunityShortsOps(ctx context.Context) (YouTubeCommunityShortsOpsResponse, error) {
	return getOwned[YouTubeCommunityShortsOpsResponse](ctx, c, "/api/holo/stats/youtube/community-shorts", nil)
}
