package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

type stubYouTubeCommunityShortsOpsRepository struct {
	listPostSendCountsSince func(context.Context, time.Time) ([]outbox.PostSendCount, error)
}

func (s *stubYouTubeCommunityShortsOpsRepository) ListPostSendCountsSince(
	ctx context.Context,
	since time.Time,
) ([]outbox.PostSendCount, error) {
	if s.listPostSendCountsSince == nil {
		return nil, nil
	}
	return s.listPostSendCountsSince(ctx, since)
}

func TestStatsHandler_GetYouTubeCommunityShortsOps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	withinTarget := false
	exceeded := true
	firstPublishedAt := now.Add(-20 * time.Minute)
	secondPublishedAt := now.Add(-10 * time.Minute)
	thirdPublishedAt := now.Add(-90 * time.Minute)
	firstAlarmSentAt := firstPublishedAt.Add(60 * time.Second)
	thirdAlarmSentAt := thirdPublishedAt.Add(90 * time.Second)
	firstLatencyMillis := int64(60000)
	secondLatencyMillis := int64(180000)
	thirdLatencyMillis := int64(90000)
	firstEventAt := firstPublishedAt.Add(45 * time.Second)
	secondEventAt := secondPublishedAt.Add(2 * time.Minute)
	thirdEventAt := thirdPublishedAt.Add(70 * time.Second)

	handler := &StatsHandler{Handler: &Handler{
		communityShortsOps: &stubYouTubeCommunityShortsOpsRepository{
			listPostSendCountsSince: func(_ context.Context, since time.Time) ([]outbox.PostSendCount, error) {
				if since.IsZero() {
					t.Fatal("since must be set")
				}
				return []outbox.PostSendCount{
					{
						AlarmType:            domain.AlarmTypeCommunity,
						ChannelID:            "channel-1",
						ContentID:            "community-1",
						ActualPublishedAt:    &firstPublishedAt,
						AlarmSentAt:          &firstAlarmSentAt,
						AlarmLatencyMillis:   &firstLatencyMillis,
						AlarmLatencyExceeded: &withinTarget,
						OutboxCount:          1,
						SuccessSendCount:     1,
						SuccessRoomCount:     1,
						FirstEventAt:         &firstEventAt,
						LastEventAt:          &firstEventAt,
						FirstSuccessAt:       &firstEventAt,
						LastSuccessAt:        &firstEventAt,
					},
					{
						AlarmType:            domain.AlarmTypeShorts,
						ChannelID:            "channel-1",
						ContentID:            "shorts-1",
						ActualPublishedAt:    &secondPublishedAt,
						AlarmLatencyMillis:   &secondLatencyMillis,
						AlarmLatencyExceeded: &exceeded,
						OutboxCount:          1,
						FailedAttemptCount:   1,
						FirstEventAt:         &secondEventAt,
						LastEventAt:          &secondEventAt,
					},
					{
						AlarmType:            domain.AlarmTypeCommunity,
						ChannelID:            "channel-2",
						ContentID:            "community-2",
						ActualPublishedAt:    &thirdPublishedAt,
						AlarmSentAt:          &thirdAlarmSentAt,
						AlarmLatencyMillis:   &thirdLatencyMillis,
						AlarmLatencyExceeded: &withinTarget,
						OutboxCount:          1,
						SuccessSendCount:     1,
						SuccessRoomCount:     1,
						FirstEventAt:         &thirdEventAt,
						LastEventAt:          &thirdEventAt,
						FirstSuccessAt:       &thirdEventAt,
						LastSuccessAt:        &thirdEventAt,
					},
				}, nil
			},
		},
		memberIndexLoader: func(context.Context) ([]*domain.Member, error) {
			return []*domain.Member{{ChannelID: "channel-1", Name: "Mio"}, {ChannelID: "channel-2", Name: "Sora"}}, nil
		},
		logger: newDiscardLogger(),
	}}

	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/youtube/community-shorts", nil)
	handler.GetYouTubeCommunityShortsOps(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response YouTubeCommunityShortsOpsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response.WindowHours != youtubeCommunityShortsOpsWindowHours {
		t.Fatalf("windowHours=%d want=%d", response.WindowHours, youtubeCommunityShortsOpsWindowHours)
	}
	if response.Overview.ChannelCount != 2 {
		t.Fatalf("channelCount=%d want=2", response.Overview.ChannelCount)
	}
	if response.Overview.DetectedPostCount != 3 {
		t.Fatalf("detectedPostCount=%d want=3", response.Overview.DetectedPostCount)
	}
	if response.Overview.SuccessPostCount != 2 {
		t.Fatalf("successPostCount=%d want=2", response.Overview.SuccessPostCount)
	}
	if response.Overview.DetectedUnsentPostCount != 1 {
		t.Fatalf("detectedUnsentPostCount=%d want=1", response.Overview.DetectedUnsentPostCount)
	}
	if response.Overview.PendingPostCount != 1 {
		t.Fatalf("pendingPostCount=%d want=1", response.Overview.PendingPostCount)
	}
	if response.Overview.ExceededPostCount != 1 {
		t.Fatalf("exceededPostCount=%d want=1", response.Overview.ExceededPostCount)
	}
	if response.Overview.CommunityDetectedPostCount != 2 {
		t.Fatalf("communityDetectedPostCount=%d want=2", response.Overview.CommunityDetectedPostCount)
	}
	if response.Overview.ShortsDetectedPostCount != 1 {
		t.Fatalf("shortsDetectedPostCount=%d want=1", response.Overview.ShortsDetectedPostCount)
	}
	if response.Overview.AverageLatencyMillis == nil || *response.Overview.AverageLatencyMillis != 110000 {
		t.Fatalf("averageLatencyMillis=%v want=110000", response.Overview.AverageLatencyMillis)
	}
	if response.Overview.MaxLatencyMillis == nil || *response.Overview.MaxLatencyMillis != secondLatencyMillis {
		t.Fatalf("maxLatencyMillis=%v want=%d", response.Overview.MaxLatencyMillis, secondLatencyMillis)
	}
	if len(response.Channels) != 2 {
		t.Fatalf("channels=%d want=2", len(response.Channels))
	}
	if response.Channels[0].ChannelID != "channel-1" {
		t.Fatalf("first channel=%s want=channel-1", response.Channels[0].ChannelID)
	}
	if response.Channels[0].MemberName != "Mio" {
		t.Fatalf("first memberName=%s want=Mio", response.Channels[0].MemberName)
	}
	if response.Channels[0].ExceededPostCount != 1 {
		t.Fatalf("first exceededPostCount=%d want=1", response.Channels[0].ExceededPostCount)
	}
	if response.Channels[0].PendingPostCount != 1 {
		t.Fatalf("first pendingPostCount=%d want=1", response.Channels[0].PendingPostCount)
	}
	if response.Channels[1].MemberName != "Sora" {
		t.Fatalf("second memberName=%s want=Sora", response.Channels[1].MemberName)
	}
}

func TestStatsHandler_GetYouTubeCommunityShortsOps_RepositoryUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &StatsHandler{Handler: &Handler{logger: newDiscardLogger()}}
	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/youtube/community-shorts", nil)
	handler.GetYouTubeCommunityShortsOps(ctx)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "YouTube community/shorts ops repository not available")
}

func TestStatsHandler_GetYouTubeCommunityShortsOps_RepositoryError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &StatsHandler{Handler: &Handler{
		communityShortsOps: &stubYouTubeCommunityShortsOpsRepository{
			listPostSendCountsSince: func(context.Context, time.Time) ([]outbox.PostSendCount, error) {
				return nil, errors.New("boom")
			},
		},
		logger: newDiscardLogger(),
	}}

	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/youtube/community-shorts", nil)
	handler.GetYouTubeCommunityShortsOps(ctx)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "Failed to load YouTube community/shorts ops posts")
}
