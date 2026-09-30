package checking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type unstagedYouTubeCandidates struct {
	work          youtubeChannelCheckWork
	notifications []*domain.AlarmNotification
	evaluatedAt   time.Time
}

func (c *YouTubeChecker) stageUpcomingChannel(ctx context.Context, work *youtubeChannelCheckWork, now time.Time, notifications []*domain.AlarmNotification) error {
	if c.upcomingCandidates == nil {
		return nil
	}

	batch := &unstagedYouTubeCandidates{work: *work, notifications: notifications, evaluatedAt: now}
	// 실패한 실행의 선정 결과는 다음 조회 전에 재저장한다. 한 cycle의 채널별 결과만 보유한다.
	c.unstagedMu.Lock()

	if c.unstagedUpcoming == nil {
		c.unstagedUpcoming = make(map[string]*unstagedYouTubeCandidates)
	}

	c.unstagedUpcoming[work.channelID] = batch
	c.unstagedMu.Unlock()

	if err := c.upcomingCandidates.Stage(ctx, work.channelID, now, notifications); err != nil {
		return fmt.Errorf("stage upcoming channel: %w", err)
	}

	c.forgetUnstagedUpcoming(work.channelID, batch)

	return nil
}

func (c *YouTubeChecker) forgetUnstagedUpcoming(channelID string, batch *unstagedYouTubeCandidates) {
	c.unstagedMu.Lock()
	defer c.unstagedMu.Unlock()

	if c.unstagedUpcoming[channelID] == batch {
		delete(c.unstagedUpcoming, channelID)
	}
}

func (c *YouTubeChecker) flushUnstagedUpcoming(ctx context.Context) error {
	c.unstagedMu.Lock()

	batches := make([]*unstagedYouTubeCandidates, 0, len(c.unstagedUpcoming))
	for _, batch := range c.unstagedUpcoming {
		batches = append(batches, batch)
	}

	c.unstagedMu.Unlock()

	for _, batch := range batches {
		if err := c.upcomingCandidates.Stage(ctx, batch.work.channelID, batch.evaluatedAt, batch.notifications); err != nil {
			return fmt.Errorf("flush upcoming channel: %w", err)
		}

		c.forgetUnstagedUpcoming(batch.work.channelID, batch)
	}

	return nil
}

func (c *YouTubeChecker) recoverUpcomingAfterFailure(ctx context.Context, streams map[string][]*domain.Stream, now time.Time, checkErr error) ([]*domain.AlarmNotification, error) {
	if c.upcomingCandidates == nil {
		return nil, checkErr
	}

	recovered, err := c.recoverUpcomingCandidates(ctx, streams, now)

	return recovered, errors.Join(checkErr, err)
}
