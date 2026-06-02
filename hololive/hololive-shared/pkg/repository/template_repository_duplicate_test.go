package repository

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateRepository_CreateTemplateRecoversFromDuplicateKey(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := NewTemplateRepository(pool, logger)
	ctx := context.Background()
	key := domain.TemplateKeyOutboxCommunity

	_, err := pool.Exec(ctx,
		`INSERT INTO notification_templates(template_key, channel_id, body)
		 VALUES ($1, NULL, $2)
		 ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body`,
		key,
		"racing body",
	)
	require.NoError(t, err)

	tmpl, err := repository.createTemplate(ctx, key, nil, "resolved body")
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Equal(t, "resolved body", tmpl.Body)

	found, err := repository.FindByKeyAndChannel(ctx, key, nil)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "resolved body", found.Body)
}
