package scheduleposter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type OfficialStream struct {
	VideoID   string
	Title     string
	StartsAt  time.Time
	Guests    []string
	ChannelID string
}

func (s *Store) OfficialStreams(ctx context.Context, videoIDs []string) (map[string]OfficialStream, error) {
	if len(videoIDs) == 0 {
		return map[string]OfficialStream{}, nil
	}

	rows, err := s.pool.Query(ctx, officialStreamsSQL, videoIDs)
	if err != nil {
		return nil, fmt.Errorf("load official streams: %w", err)
	}

	streams, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OfficialStream])
	if err != nil {
		return nil, fmt.Errorf("load official streams: %w", err)
	}

	found := make(map[string]OfficialStream, len(streams))
	for i := range streams {
		found[streams[i].VideoID] = streams[i]
	}

	return found, nil
}
