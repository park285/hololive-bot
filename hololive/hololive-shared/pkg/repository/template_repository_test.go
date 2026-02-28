package repository_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&domain.NotificationTemplate{}, &domain.NotificationTemplateRevision{})
	require.NoError(t, err)

	err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_notification_templates_default
		ON notification_templates(template_key)
		WHERE channel_id IS NULL
	`).Error
	require.NoError(t, err)

	err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_notification_templates_channel
		ON notification_templates(template_key, channel_id)
		WHERE channel_id IS NOT NULL
	`).Error
	require.NoError(t, err)

	return db
}

func TestTemplateRepository_List(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	t.Run("empty list", func(t *testing.T) {
		templates, err := repo.List(ctx, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("with data and filters", func(t *testing.T) {
		db.Create(&domain.NotificationTemplate{
			TemplateKey: domain.TemplateKeyOutboxShorts,
			Body:        "default body",
		})
		channelID := "room_123"
		db.Create(&domain.NotificationTemplate{
			TemplateKey: domain.TemplateKeyOutboxShorts,
			ChannelID:   &channelID,
			Body:        "override body",
		})

		templates, err := repo.List(ctx, nil, nil)
		require.NoError(t, err)
		assert.Len(t, templates, 2)

		key := domain.TemplateKeyOutboxShorts
		templates, err = repo.List(ctx, &key, nil)
		require.NoError(t, err)
		assert.Len(t, templates, 2)

		templates, err = repo.List(ctx, &key, &channelID)
		require.NoError(t, err)
		assert.Len(t, templates, 1)
		assert.Equal(t, "room_123", *templates[0].ChannelID)
	})
}

func TestTemplateRepository_Upsert(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	t.Run("insert default", func(t *testing.T) {
		tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "new body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, domain.TemplateKeyOutboxShorts, tmpl.TemplateKey)
		assert.Nil(t, tmpl.ChannelID)
		assert.Equal(t, "new body", tmpl.Body)
	})

	t.Run("update default", func(t *testing.T) {
		_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "updated body")
		require.NoError(t, err)

		found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		require.NoError(t, err)
		assert.Equal(t, "updated body", found.Body)
	})

	t.Run("insert override", func(t *testing.T) {
		channelID := "room_abc"
		tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, "room_abc", *tmpl.ChannelID)
	})

	t.Run("update override", func(t *testing.T) {
		channelID := "room_abc"
		_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override updated")
		require.NoError(t, err)

		found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
		require.NoError(t, err)
		assert.Equal(t, "override updated", found.Body)
	})

	t.Run("recover from duplicate key during create", func(t *testing.T) {
		const callbackName = "test:inject-template-duplicate"
		injected := false
		key := domain.TemplateKeyOutboxCommunity

		err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
			if injected {
				return
			}

			tmpl, ok := tx.Statement.Dest.(*domain.NotificationTemplate)
			if !ok {
				return
			}
			if tmpl.TemplateKey != key || tmpl.ChannelID != nil {
				return
			}

			injected = true
			if execErr := tx.Exec(
				`INSERT INTO notification_templates(template_key, channel_id, body, created_at, updated_at) VALUES (?, NULL, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
				key,
				"racing body",
			).Error; execErr != nil {
				tx.AddError(execErr)
			}
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = db.Callback().Create().Remove(callbackName)
		})

		tmpl, err := repo.Upsert(ctx, key, nil, "resolved body")
		require.NoError(t, err)
		require.NotNil(t, tmpl)
		assert.Equal(t, "resolved body", tmpl.Body)

		found, err := repo.FindByKeyAndChannel(ctx, key, nil)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "resolved body", found.Body)

		var count int64
		err = db.Model(&domain.NotificationTemplate{}).
			Where("template_key = ? AND channel_id IS NULL", key).
			Count(&count).Error
		require.NoError(t, err)
		assert.Equal(t, int64(1), count)
	})
}

func TestTemplateRepository_DeleteOverride(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	channelID := "room_del"
	_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxCommunity, &channelID, "to delete")
	require.NoError(t, err)

	err = repo.DeleteOverride(ctx, domain.TemplateKeyOutboxCommunity, channelID)
	require.NoError(t, err)

	found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxCommunity, &channelID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestTemplateRepository_GetByKey(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, nil, "default video")
	require.NoError(t, err)

	ch1 := "room_1"
	_, err = repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch1, "override 1")
	require.NoError(t, err)

	ch2 := "room_2"
	_, err = repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch2, "override 2")
	require.NoError(t, err)

	defaultTmpl, overrides, err := repo.GetByKey(ctx, domain.TemplateKeyOutboxVideo)
	require.NoError(t, err)
	require.NotNil(t, defaultTmpl)
	assert.Equal(t, "default video", defaultTmpl.Body)
	assert.Len(t, overrides, 2)
}

func TestTemplateRepository_Revisions(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxMilestone, nil, "v1")
	require.NoError(t, err)

	err = repo.CreateRevision(ctx, tmpl.ID, "v0 old body")
	require.NoError(t, err)
	err = repo.CreateRevision(ctx, tmpl.ID, "v0.5 older body")
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)
	assert.Len(t, revisions, 2)

	rev, err := repo.GetRevisionByID(ctx, revisions[0].ID)
	require.NoError(t, err)
	assert.NotNil(t, rev)
}

func TestTemplateRepository_PruneOldRevisions(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	ctx := context.Background()

	tmpl, err := repo.Upsert(ctx, domain.TemplateKeyCmdHelp, nil, "help")
	require.NoError(t, err)

	for range 10 {
		err = repo.CreateRevision(ctx, tmpl.ID, "revision body")
		require.NoError(t, err)
	}

	err = repo.PruneOldRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 5)
}
