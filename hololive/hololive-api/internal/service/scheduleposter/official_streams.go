package scheduleposter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// OfficialStream은 공식 일정이 콜라보 출연자를 적은 방송이다. Guests는 공식 일정의 표기 그대로다.
// ChannelID는 방송을 여는 채널이며 알 수 없으면 비어 있다.
type OfficialStream struct {
	VideoID   string
	Title     string
	StartsAt  time.Time
	Guests    []string
	ChannelID string
}

// OfficialStreams는 videoIDs 가운데 공식 일정이 콜라보 출연자를 적은 방송을 영상 ID로 찾아 돌려준다.
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
