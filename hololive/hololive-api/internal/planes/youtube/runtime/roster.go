package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type rosterReader struct {
	timeFinalizationBacklogBatchSize int
}

func (rosterReader) NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, notificationChannelIDsSQL, "notification")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

func (rosterReader) OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error) {
	out, err := loadChannelIDs(ctx, tx, operationalChannelIDsSQL, "operational")
	if err != nil {
		return out, fmt.Errorf("load channel IDs: %w", err)
	}

	return out, nil
}

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

	rows, err := pgxdb.Query(ctx, tx, liveCheckVideosSQL,
		query.OperationalChannelIDs,
		targetprojection.MaxInputLiveCheckVideoCount+1)
	if err != nil {
		return nil, fmt.Errorf("%w: load live check videos: %w", targetprojection.ErrInputRead, err)
	}
	defer rows.Close()

	videos := make([]targetprojection.LiveCheckVideo, 0)

	for rows.Next() {
		var (
			video             targetprojection.LiveCheckVideo
			facts             liveCheckFreshness
			unresolvableSince pgtype.Timestamptz
		)

		if err := rows.Scan(&video.VideoID, &video.ChannelID, &video.IsUpcoming, &facts.asOf,
			&facts.positiveAt, &facts.positiveSeenAt, &facts.availabilityAt, &facts.availabilitySeenAt,
			&unresolvableSince); err != nil {
			return nil, fmt.Errorf("%w: scan live check video: %w", targetprojection.ErrInputRead, err)
		}

		video.NotBefore = facts.notBefore(query.FreshnessBudget)
		video.UnresolvableFor = unresolvableFor(facts.asOf, unresolvableSince)

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

func (r rosterReader) TimeFinalizationVideos(ctx context.Context, tx dbx.Tx) ([]targetprojection.TimeFinalizationVideo, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}

	const (
		maxNewFinalizations     = 300
		maxBacklogFinalizations = 2000
	)

	rows, err := pgxdb.Query(ctx, tx, timeFinalizationVideosSQL, r.timeFinalizationBacklogBatchSize,
		maxNewFinalizations, maxBacklogFinalizations,
		sourceobservation.TimeFinalizationNewWindow, sourceobservation.TimeFinalizationFirstAttemptDelay)
	if err != nil {
		return nil, fmt.Errorf("%w: load time finalization videos: %w", targetprojection.ErrInputRead, err)
	}
	defer rows.Close()

	var videos []targetprojection.TimeFinalizationVideo

	for rows.Next() {
		var video targetprojection.TimeFinalizationVideo

		if err := rows.Scan(&video.VideoID, &video.ChannelID, &video.NotBefore); err != nil {
			return nil, fmt.Errorf("%w: scan time finalization video: %w", targetprojection.ErrInputRead, err)
		}

		videos = append(videos, video)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read time finalization videos: %w", targetprojection.ErrInputRead, err)
	}

	return videos, nil
}

func unresolvableFor(asOf time.Time, since pgtype.Timestamptz) time.Duration {
	if !since.Valid || !since.Time.Before(asOf) {
		return 0
	}

	return asOf.Sub(since.Time)
}

type liveCheckFreshness struct {
	asOf               time.Time
	positiveAt         pgtype.Timestamptz
	positiveSeenAt     pgtype.Timestamptz
	availabilityAt     pgtype.Timestamptz
	availabilitySeenAt pgtype.Timestamptz
}

func (facts liveCheckFreshness) notBefore(budget time.Duration) time.Time {
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

func loadChannelIDs(ctx context.Context, tx dbx.Tx, query pgxdb.Statement, label string) ([]string, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is not configured", targetprojection.ErrInputRead)
	}

	rows, err := pgxdb.Query(ctx, tx, query, targetprojection.MaxInputChannelCount+1)
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
