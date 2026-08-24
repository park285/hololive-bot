package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
)

type stubYouTubeCommunityShortsOpsRepository struct {
	listPostSendCountsSince func(context.Context, time.Time) ([]analytics.PostSendCount, error)
}

func (s *stubYouTubeCommunityShortsOpsRepository) ListPostSendCountsSince(
	ctx context.Context,
	since time.Time,
) ([]analytics.PostSendCount, error) {
	if s.listPostSendCountsSince == nil {
		return nil, nil
	}

	out, err := s.listPostSendCountsSince(ctx, since)
	if err != nil {
		return out, fmt.Errorf("list post send counts since: %w", err)
	}

	return out, nil
}

func TestStatsHandler_GetYouTubeCommunityShortsOps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, secondLatencyMillis := newYouTubeCommunityShortsOpsTestHandler(t)
	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/youtube/community-shorts", nil)
	handler.GetYouTubeCommunityShortsOps(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response YouTubeCommunityShortsOpsResponse

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	assertYouTubeCommunityShortsOpsResponse(t, &response, secondLatencyMillis)
}

const youTubeOpsSecondLatencyMillis = int64(180000)

func newYouTubeCommunityShortsOpsTestHandler(t *testing.T) (handler *StatsHandler, secondLatencyMillis int64) {
	t.Helper()

	now := time.Now().UTC()

	handler = &StatsHandler{Handler: &Handler{
		communityShortsOps: &stubYouTubeCommunityShortsOpsRepository{
			listPostSendCountsSince: func(_ context.Context, since time.Time) ([]analytics.PostSendCount, error) {
				if since.IsZero() {
					t.Fatal("since must be set")
				}

				return youTubeCommunityShortsOpsCounts(now), nil
			},
		},
		memberIndexLoader: func(context.Context) ([]*domain.Member, error) {
			return []*domain.Member{{ChannelID: "channel-1", Name: "Mio"}, {ChannelID: "channel-2", Name: "Sora"}}, nil
		},
		logger: newDiscardLogger(),
	}}

	return handler, youTubeOpsSecondLatencyMillis
}

func youTubeCommunityShortsOpsCounts(now time.Time) []analytics.PostSendCount {
	withinTarget := false
	exceeded := true
	firstPublishedAt := now.Add(-20 * time.Minute)
	secondPublishedAt := now.Add(-10 * time.Minute)
	thirdPublishedAt := now.Add(-90 * time.Minute)
	firstAlarmSentAt := firstPublishedAt.Add(60 * time.Second)
	thirdAlarmSentAt := thirdPublishedAt.Add(90 * time.Second)
	firstLatencyMillis := int64(60000)
	secondLatencyMillis := youTubeOpsSecondLatencyMillis
	thirdLatencyMillis := int64(90000)
	firstEventAt := firstPublishedAt.Add(45 * time.Second)
	secondEventAt := secondPublishedAt.Add(2 * time.Minute)
	thirdEventAt := thirdPublishedAt.Add(70 * time.Second)

	return []analytics.PostSendCount{
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
	}
}

func assertYouTubeCommunityShortsOpsResponse(
	t *testing.T,
	response *YouTubeCommunityShortsOpsResponse,
	secondLatencyMillis int64,
) {
	t.Helper()

	assertYouTubeOpsOverviewCounts(t, response)
	assertYouTubeOpsLatency(t, response, secondLatencyMillis)
	assertYouTubeOpsChannels(t, response)
}

func assertYouTubeOpsOverviewCounts(t *testing.T, response *YouTubeCommunityShortsOpsResponse) {
	t.Helper()

	overview := response.Overview

	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"windowHours", int64(response.WindowHours), youtubeCommunityShortsOpsWindowHours},
		{"channelCount", overview.ChannelCount, 2},
		{"detectedPostCount", overview.DetectedPostCount, 3},
		{"successPostCount", overview.SuccessPostCount, 2},
		{"detectedUnsentPostCount", overview.DetectedUnsentPostCount, 1},
		{"pendingPostCount", overview.PendingPostCount, 1},
		{"exceededPostCount", overview.ExceededPostCount, 1},
		{"communityDetectedPostCount", overview.CommunityDetectedPostCount, 2},
		{"shortsDetectedPostCount", overview.ShortsDetectedPostCount, 1},
	}

	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("%s=%d want=%d", check.name, check.got, check.want)
		}
	}
}

func assertYouTubeOpsLatency(t *testing.T, response *YouTubeCommunityShortsOpsResponse, secondLatencyMillis int64) {
	t.Helper()

	overview := response.Overview

	if overview.AverageLatencyMillis == nil || *overview.AverageLatencyMillis != 110000 {
		t.Fatalf("averageLatencyMillis=%v want=110000", overview.AverageLatencyMillis)
	}

	if overview.MaxLatencyMillis == nil || *overview.MaxLatencyMillis != secondLatencyMillis {
		t.Fatalf("maxLatencyMillis=%v want=%d", overview.MaxLatencyMillis, secondLatencyMillis)
	}
}

func assertYouTubeOpsChannels(t *testing.T, response *YouTubeCommunityShortsOpsResponse) {
	t.Helper()

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
			listPostSendCountsSince: func(context.Context, time.Time) ([]analytics.PostSendCount, error) {
				return nil, errors.New("boom")
			},
		},
		logger: newDiscardLogger(),
	}}

	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/youtube/community-shorts", nil)
	handler.GetYouTubeCommunityShortsOps(ctx)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "Failed to load YouTube community/shorts ops posts")
}
