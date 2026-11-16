package scheduleposter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

func TestStoreOfficialStreamsReturnsCollabsWithTheirChannel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, pool := newRowsStore(t)
	startsAt := time.Date(2026, time.November, 17, 10, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_schedule_items (group_key, provider, external_id, video_id, channel_id, title, scheduled_at, collabo_talent_names, updated_at)
		VALUES ('g', 'hololive_official', 'collab', 'collab', '', 'クッキーバトル', $1, '{AZKi,熱千めら}', now()),
		       ('old', 'hololive_official', 'collab', 'collab', '', '古い題', $1, '{AZKi}', now() - interval '1 day'),
		       ('g', 'holodex', 'collab', 'collab', 'UChost', 'クッキーバトル', $1, '{}', now()),
		       ('g', 'hololive_official', 'solo', 'solo', '', '歌枠', $1, '{}', now()),
		       ('g', 'holodex', 'solo', 'solo', 'UCsolo', '歌枠', $1, '{}', now()),
		       ('g', 'hololive_official', 'nohost', 'nohost', '', '公式番組', $1, '{熱千めら}', now()),
		       ('g', 'holodex', 'unlisted', 'unlisted', 'UCother', '雑談', $1, '{}', now())`, startsAt)
	require.NoError(t, err)

	streams, err := store.OfficialStreams(ctx, []string{"collab", "solo", "nohost", "unlisted", "missing"})
	require.NoError(t, err)

	for id, stream := range streams {
		stream.StartsAt = stream.StartsAt.UTC()
		streams[id] = stream
	}

	require.Equal(t, map[string]scheduleposter.OfficialStream{
		"collab": {VideoID: "collab", Title: "クッキーバトル", StartsAt: startsAt, Guests: []string{"AZKi", "熱千めら"}, ChannelID: "UChost"},
		"nohost": {VideoID: "nohost", Title: "公式番組", StartsAt: startsAt, Guests: []string{"熱千めら"}},
	}, streams, "출연자가 없거나 공식 일정에 없는 방송은 빼고, 여는 채널은 다른 출처 행에서 읽는다")
}
