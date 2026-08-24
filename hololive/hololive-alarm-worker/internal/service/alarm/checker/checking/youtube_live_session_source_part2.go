package checking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type officialScheduleCollaboTalentRow struct {
	VideoID            string   `db:"video_id"`
	CollaboTalentNames []string `db:"collabo_talent_names"`
}

func (s *PgYouTubeLiveSessionSource) attachOfficialCollaboTalentNames(
	ctx context.Context,
	sessions []PersistedYouTubeLiveSession,
) error {
	videoIDs := persistedSessionVideoIDs(sessions)
	if len(videoIDs) == 0 {
		return nil
	}

	namesByVideo, err := s.loadOfficialCollaboTalentNames(ctx, videoIDs)
	if err != nil {
		return fmt.Errorf("load official collabo talent names: %w", err)
	}

	for i := range sessions {
		stream := sessions[i].Stream
		if stream == nil {
			continue
		}

		names, ok := namesByVideo[stream.ID]
		if !ok {
			continue
		}

		stream.CollaboTalentNames = names
	}

	return nil
}

func persistedSessionVideoIDs(sessions []PersistedYouTubeLiveSession) []string {
	videoIDs := make([]string, 0, len(sessions))
	for i := range sessions {
		if sessions[i].Stream == nil {
			continue
		}

		videoID := strings.TrimSpace(sessions[i].Stream.ID)
		if videoID == "" {
			continue
		}

		videoIDs = append(videoIDs, videoID)
	}

	return UniqueStrings(videoIDs)
}

func (s *PgYouTubeLiveSessionSource) loadOfficialCollaboTalentNames(
	ctx context.Context,
	videoIDs []string,
) (map[string][]string, error) {
	var rows []officialScheduleCollaboTalentRow

	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_schedule_collabo_talents_0232_05.sql"), videoIDs); err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}

	namesByVideo := make(map[string][]string, len(rows))
	for i := range rows {
		videoID := strings.TrimSpace(rows[i].VideoID)
		if videoID == "" {
			continue
		}

		namesByVideo[videoID] = cloneStringSlice(rows[i].CollaboTalentNames)
	}

	return namesByVideo, nil
}

func persistedLiveStatusToStreamStatus(status domain.LiveStatus) (domain.StreamStatus, bool) {
	switch status {
	case domain.LiveStatusLive:
		return domain.StreamStatusLive, true
	case domain.LiveStatusUpcoming:
		return domain.StreamStatusUpcoming, true
	case domain.LiveStatusEnded:
		return "", false
	default:
		return "", false
	}
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}

	utc := value.UTC()

	return &utc
}

func utcTimeValue(value *time.Time) time.Time {
	if value == nil || value.IsZero() {
		return time.Time{}
	}

	return value.UTC()
}
