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

package batchrepo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// TestIsCommunityShortsOutboxKindRearmPolicy는 poll-persist의 community/shorts 전용 게이트를
// 전 OutboxKind에 대해 고정한다. 이 헬퍼는 "poll 재발견 rearm"(ON CONFLICT)과 community/shorts 전용
// 처리만 게이팅하며, community/shorts만 대상인 것은 watermark 보류로 이들만 재폴링에서 재등장하기
// 때문이다(video/live는 watermark 전진으로 재등장 없음). video/live의 미발송 FAILED 복구는 dispatcher의
// revival sweep(별도 경로)이 담당하므로, 이 헬퍼가 video/live를 false로 두는 것은 유실 정책이 아니다.
func TestIsCommunityShortsOutboxKindRearmPolicy(t *testing.T) {
	cases := []struct {
		kind  domain.OutboxKind
		rearm bool
	}{
		{domain.OutboxKindCommunityPost, true},
		{domain.OutboxKindNewShort, true},
		{domain.OutboxKindNewVideo, false},
		{domain.OutboxKindLiveStream, false},
		{domain.OutboxKindMilestone, false},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			require.Equal(t, tc.rearm, isCommunityShortsOutboxKind(tc.kind),
				"rearm policy for kind %q must stay %v (D6: 콘텐츠 불변 kind는 재무장 금지)", tc.kind, tc.rearm)
		})
	}
}

// TestPgxBatchRepositoryPersistVideosDoesNotReactivateFailedLiveStreamOutbox는 poll-persist의
// ON CONFLICT 경로가 LIVE_STREAM을 되살리지 않음을 고정한다(기존 NEW_VIDEO 케이스의 LIVE 미러).
// 이는 poll 재발견 rearm이 video/live에 도달하지 않는다는 경계를 문서화하는 것으로, video/live의
// 미발송 FAILED 복구는 dispatcher revival sweep이 별도로 담당한다(dispatcher_claim_revive_test.go).
func TestPgxBatchRepositoryPersistVideosDoesNotReactivateFailedLiveStreamOutbox(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()

	createdAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	nextAttemptAt := createdAt.Add(5 * time.Minute)
	existingOutbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindLiveStream,
		ChannelID:     "channel-1",
		ContentID:     "live-1",
		Payload:       `{"video_id":"live-1","version":"old"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     createdAt,
		Error:         "live failed",
	}
	require.NoError(t, db.Create(&existingOutbox).Error)

	err := persistVideos(repository, ctx, []*domain.YouTubeVideo{{
		VideoID:      "live-1",
		ChannelID:    "channel-1",
		Title:        "title-live-1",
		IsLiveReplay: true,
		ViewCount:    999,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindLiveStream,
		ChannelID: "channel-1",
		ContentID: "live-1",
		Payload:   `{"video_id":"live-1","version":"new"}`,
		Status:    domain.OutboxStatusPending,
	}}, &domain.YouTubeContentWatermark{
		ChannelID:     "channel-1",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "live-1",
	})
	require.NoError(t, err)

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, existingOutbox.ID, outboxRows[0].ID)
	require.Equal(t, domain.OutboxStatusFailed, outboxRows[0].Status)
	require.Equal(t, 3, outboxRows[0].AttemptCount)
	require.Equal(t, nextAttemptAt, outboxRows[0].NextAttemptAt.UTC())
	require.Equal(t, "live failed", outboxRows[0].Error)
	require.Contains(t, outboxRows[0].Payload, `"version": "old"`)
}
