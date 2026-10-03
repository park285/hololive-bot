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

// Package canonicalwrite는 source observation 확정 트랜잭션 안에서 YouTube canonical 영상·커뮤니티 행,
// outbox 삽입, tracking·source post·alarm state, watermark를 저장한다. 트랜잭션은 호출자가 소유하며
// 이 패키지는 DB 핸들이나 독립 트랜잭션 경로를 두지 않는다.
package canonicalwrite

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

const batchMaxSize = 50

// PersistVideosTx는 호출자 트랜잭션 안에서 영상·쇼츠 확정 결과를 저장한다.
func PersistVideosTx(
	ctx context.Context,
	tx dbx.Tx,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if tx == nil {
		return errors.New("persist videos: tx is nil")
	}

	if err := validateShortNotificationPublishedAt(videos, notifications); err != nil {
		return fmt.Errorf("validate short notifications: %w", err)
	}

	if err := persistReconciledVideosTx(ctx, tx, videos, notifications, trackingRows, watermark); err != nil {
		return fmt.Errorf("persist reconciled videos tx: %w", err)
	}

	return nil
}

func persistReconciledVideosTx(
	ctx context.Context,
	tx dbx.Querier,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	// 종류별 reducer 상태에 없는 영상도 다른 종류로 이미 저장되어 있으면 Shorts로 소급 알리지 않습니다.
	notifications, trackingRows, err := dropAlreadyKnownShortArtifacts(ctx, tx, notifications, trackingRows)
	if err != nil {
		return fmt.Errorf("drop already-known short artifacts: %w", err)
	}

	if err := batchUpsertVideos(ctx, tx, videos); err != nil {
		return fmt.Errorf("batch upsert videos: %w", err)
	}

	canonicalizeShortContentIDs(notifications, trackingRows)

	sourcePosts := buildShortSourcePosts(videos, trackingRows)
	if err := observation.NewRepositoryContext(ctx, tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert short source posts: %w", err)
	}

	if err := persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "video", "short"); err != nil {
		return fmt.Errorf("persist tracking and watermark: %w", err)
	}

	return nil
}

// PersistCommunityPostsTx는 호출자 트랜잭션 안에서 커뮤니티 게시글 확정 결과를 저장한다.
func PersistCommunityPostsTx(
	ctx context.Context,
	tx dbx.Tx,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if tx == nil {
		return errors.New("persist community posts: tx is nil")
	}

	if err := validateCommunityNotificationPublishedAt(posts, notifications); err != nil {
		return fmt.Errorf("validate community notifications: %w", err)
	}

	if err := persistCommunityPostsTx(ctx, tx, posts, notifications, trackingRows, watermark); err != nil {
		return fmt.Errorf("persist community posts tx: %w", err)
	}

	return nil
}

func persistCommunityPostsTx(
	ctx context.Context,
	tx dbx.Querier,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
		return fmt.Errorf("batch upsert community posts: %w", err)
	}

	sourcePosts := buildCommunitySourcePosts(posts, trackingRows)
	if err := observation.NewRepositoryContext(ctx, tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert community source posts: %w", err)
	}

	if err := persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "community", "community"); err != nil {
		return fmt.Errorf("persist tracking and watermark: %w", err)
	}

	return nil
}

func persistTrackingAndWatermark(
	ctx context.Context,
	tx dbx.Querier,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
	trackingLabel string,
	alarmStateLabel string,
) error {
	if err := batchInsertNotifications(ctx, tx, notifications); err != nil {
		return fmt.Errorf("batch insert notifications: %w", err)
	}

	if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
		return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
	}

	if err := observation.NewRepositoryContext(ctx, tx).UpsertBatch(ctx, trackingRows); err != nil {
		return fmt.Errorf("upsert %s tracking: %w", trackingLabel, err)
	}

	alarmStates := buildCommunityShortsAlarmStates(trackingRows)
	if err := observation.NewRepositoryContext(ctx, tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
		return fmt.Errorf("upsert %s alarm states: %w", alarmStateLabel, err)
	}

	if err := upsertWatermark(ctx, tx, watermark); err != nil {
		return fmt.Errorf("upsert watermark: %w", err)
	}

	return nil
}
