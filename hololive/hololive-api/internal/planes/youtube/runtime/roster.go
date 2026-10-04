package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type rosterReader struct{}

func (rosterReader) NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, "notification_channel_ids.sql", "notification")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

func (rosterReader) OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, "operational_channel_ids.sql", "operational")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

// LiveCheckVideos는 같은 projection transaction에서 운영 roster의 영상 확인 구조 membership과
// 조회 statement의 DB 시각과 사실로 not_before를 계산합니다. 상한 초과는 절단하지 않고 거부합니다.
func (rosterReader) LiveCheckVideos(
	ctx context.Context,
	tx dbx.Tx,
	query targetprojection.LiveCheckVideoQuery,
) ([]targetprojection.LiveCheckVideo, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}

	if query.FreshnessBudget <= 0 {
		return nil, fmt.Errorf("%w: live check video freshness budget is invalid", targetprojection.ErrInvalidProjection)
	}

	if len(query.OperationalChannelIDs) == 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, mustSQL("live_check_videos.sql"),
		query.OperationalChannelIDs,
		targetprojection.MaxInputLiveCheckVideoCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load live check videos: %w", targetprojection.ErrInputRead, err)
	}
	defer rows.Close()

	videos := make([]targetprojection.LiveCheckVideo, 0)

	for rows.Next() {
		var (
			video targetprojection.LiveCheckVideo
			facts liveCheckFreshness
		)

		if err := rows.Scan(&video.VideoID, &video.ChannelID, &video.IsUpcoming, &facts.asOf,
			&facts.positiveAt, &facts.positiveSeenAt, &facts.availabilityAt, &facts.availabilitySeenAt); err != nil {
			return nil, fmt.Errorf("%w: scan live check video: %w", targetprojection.ErrInputRead, err)
		}

		video.NotBefore = facts.notBefore(query.FreshnessBudget)

		videos = append(videos, video)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read live check videos: %w", targetprojection.ErrInputRead, err)
	}

	if len(videos) > targetprojection.MaxInputLiveCheckVideoCount {
		return nil, fmt.Errorf("%w: live check video count exceeds %d", targetprojection.ErrInvalidProjection, targetprojection.MaxInputLiveCheckVideoCount)
	}

	return videos, nil
}

// liveCheckFreshness는 LIVE positive 또는 UPCOMING positive와 UNKNOWN을 포함한 가용성
// 사실이다. SQL은 LIVE의 가용성을 제외하며, 미래 또는 NULL 시각은 신선도 근거가 아니다.
type liveCheckFreshness struct {
	asOf               time.Time
	positiveAt         pgtype.Timestamptz
	positiveSeenAt     pgtype.Timestamptz
	availabilityAt     pgtype.Timestamptz
	availabilitySeenAt pgtype.Timestamptz
}

func (facts liveCheckFreshness) notBefore(budget time.Duration) time.Time {
	// 기존 SQL의 millisecond 예산 정밀도를 보존한다.
	budget = budget.Truncate(time.Millisecond)

	positive := freshnessDeadline(facts.asOf, facts.positiveAt, facts.positiveSeenAt, budget)
	availability := freshnessDeadline(facts.asOf, facts.availabilityAt, facts.availabilitySeenAt, budget)

	if availability.After(positive) {
		return availability
	}

	return positive
}

func freshnessDeadline(asOf time.Time, effective, observed pgtype.Timestamptz, budget time.Duration) time.Time {
	if !effective.Valid || !observed.Valid || effective.Time.After(asOf) || observed.Time.After(asOf) {
		return time.Time{}
	}

	if effective.InfinityModifier != pgtype.Finite || observed.InfinityModifier != pgtype.Finite {
		return time.Time{}
	}

	earliest := effective.Time
	if observed.Time.Before(earliest) {
		earliest = observed.Time
	}

	return earliest.Add(budget)
}

func loadChannelIDs(ctx context.Context, tx dbx.Tx, queryName, label string) ([]string, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}

	rows, err := tx.Query(ctx, mustSQL(queryName), targetprojection.MaxInputChannelCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load %s channels: %w", targetprojection.ErrInputRead, label, err)
	}
	defer rows.Close()

	ids := make([]string, 0)

	for rows.Next() {
		var channelID string

		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("%w: scan %s channel: %w", targetprojection.ErrInputRead, label, err)
		}

		ids = append(ids, channelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read %s channels: %w", targetprojection.ErrInputRead, label, err)
	}

	if len(ids) > targetprojection.MaxInputChannelCount {
		return nil, fmt.Errorf("%w: %s channel count exceeds %d", targetprojection.ErrInvalidProjection, label, targetprojection.MaxInputChannelCount)
	}

	return ids, nil
}
