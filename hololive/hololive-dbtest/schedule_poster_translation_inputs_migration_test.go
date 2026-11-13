package dbtest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterTranslationInputsStayPairedAndBounded(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const insert = `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		translation_inputs_sha256, source_transcript, translation_table)
		VALUES ($1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '', $2, $3, $4)`

	key := bytes.Repeat([]byte{1}, 32)
	transcript := []string{"週間スケジュール"}
	table := new("週間スケジュール ⇒ 주간 스케줄")

	for index, row := range []struct {
		key        []byte
		transcript []string
		table      *string
	}{
		{key: nil, transcript: nil, table: nil},
		{key: key, transcript: transcript, table: table},
	} {
		_, err := pool.Exec(ctx, insert, index+1, row.key, row.transcript, row.table)
		require.NoError(t, err, "%+v", row)
	}

	for index, row := range []struct {
		key        []byte
		transcript []string
		table      *string
	}{
		{key: key, transcript: nil, table: table},
		{key: nil, transcript: transcript, table: table},
		{key: key, transcript: transcript, table: nil},
		{key: nil, transcript: nil, table: table},
		{key: key[:31], transcript: transcript, table: table},
		{key: key, transcript: []string{}, table: table},
		{key: key, transcript: make([]string, 201), table: table},
		{key: key, transcript: transcript, table: new("")},
		{key: key, transcript: transcript, table: new(strings.Repeat("a", 262145))},
	} {
		_, err := pool.Exec(ctx, insert, index+100, row.key, row.transcript, row.table)
		require.ErrorContains(t, err, "chk_schedule_posters_translation_inputs", "%+v", row)
	}
}
