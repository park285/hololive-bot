package repository

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTemplateRepository_UpsertResolvesCommittedDuplicateRow(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := NewTemplateRepository(pool, logger)
	ctx := t.Context()
	key := domain.TemplateKeyOutboxCommunity

	_, err := pool.Exec(ctx,
		`INSERT INTO notification_templates(template_key, channel_id, body)
		 VALUES ($1, NULL, $2)
		 ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body`,
		key,
		"racing body",
	)
	require.NoError(t, err)

	tmpl, err := repository.Upsert(ctx, key, nil, "resolved body")
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Equal(t, "resolved body", tmpl.Body)

	resolved, found, err := repository.FindByKeyAndChannel(ctx, key, nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "resolved body", resolved.Body)
	assert.Equal(t, tmpl.ID, resolved.ID)
}

func TestTemplateRepository_UpsertResolvesCommittedDuplicateOverrideRow(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := NewTemplateRepository(pool, logger)
	ctx := t.Context()
	key := domain.TemplateKeyOutboxCommunity
	channelID := "room_dup"

	_, err := pool.Exec(ctx,
		`INSERT INTO notification_templates(template_key, channel_id, body) VALUES ($1, $2, $3)`,
		key,
		channelID,
		"racing body",
	)
	require.NoError(t, err)

	tmpl, err := repository.Upsert(ctx, key, &channelID, "resolved body")
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Equal(t, "resolved body", tmpl.Body)
	require.NotNil(t, tmpl.ChannelID)
	assert.Equal(t, channelID, *tmpl.ChannelID)

	resolved, found, err := repository.FindByKeyAndChannel(ctx, key, &channelID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "resolved body", resolved.Body)
	assert.Equal(t, tmpl.ID, resolved.ID)
}
