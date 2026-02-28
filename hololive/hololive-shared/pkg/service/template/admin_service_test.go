package template_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&domain.NotificationTemplate{}, &domain.NotificationTemplateRevision{})
	require.NoError(t, err)

	db.Create(&domain.NotificationTemplate{
		TemplateKey: domain.TemplateKeyOutboxShorts,
		Body:        "[{{.MemberName}}] 새 쇼츠\n{{.Title | truncate 50}}\n{{.URL}}",
	})

	return db
}

func TestAdminService_List(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	templates, err := svc.List(ctx, nil, nil)
	require.NoError(t, err)
	assert.Len(t, templates, 1)
}

func TestAdminService_GetByKey(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	t.Run("existing key", func(t *testing.T) {
		defaultTmpl, overrides, err := svc.GetByKey(ctx, domain.TemplateKeyOutboxShorts)
		require.NoError(t, err)
		require.NotNil(t, defaultTmpl)
		assert.Empty(t, overrides)
	})

	t.Run("non-existing key", func(t *testing.T) {
		_, _, err := svc.GetByKey(ctx, domain.TemplateKey("INVALID"))
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_Save(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	t.Run("valid template update", func(t *testing.T) {
		tmpl, err := svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] 수정됨")
		require.NoError(t, err)
		assert.Contains(t, tmpl.Body, "수정됨")
	})

	t.Run("parse error", func(t *testing.T) {
		_, err := svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "{{.MemberName")
		assert.ErrorIs(t, err, template.ErrTemplateParseError)
	})

	t.Run("render error - invalid field", func(t *testing.T) {
		_, err := svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "{{.InvalidField}}")
		assert.ErrorIs(t, err, template.ErrTemplateRenderError)
	})

	t.Run("creates revision on update", func(t *testing.T) {
		existing, _ := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		oldBody := existing.Body

		_, err := svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v2")
		require.NoError(t, err)

		revisions, err := repo.GetRevisions(ctx, existing.ID, 5)
		require.NoError(t, err)
		assert.NotEmpty(t, revisions)
		assert.Equal(t, oldBody, revisions[0].Body)
	})

	t.Run("invalid template key", func(t *testing.T) {
		_, err := svc.Save(ctx, domain.TemplateKey("INVALID"), nil, "body")
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_DeleteOverride(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	channelID := "room_123"
	_, err := svc.Save(ctx, domain.TemplateKeyOutboxShorts, &channelID, "[커스텀]")
	require.NoError(t, err)

	t.Run("channel_id required", func(t *testing.T) {
		err := svc.DeleteOverride(ctx, domain.TemplateKeyOutboxShorts, "")
		assert.ErrorIs(t, err, template.ErrChannelIDRequired)
	})

	t.Run("successful delete", func(t *testing.T) {
		err := svc.DeleteOverride(ctx, domain.TemplateKeyOutboxShorts, channelID)
		require.NoError(t, err)

		found, _ := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
		assert.Nil(t, found)
	})
}

func TestAdminService_Preview(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	t.Run("successful preview", func(t *testing.T) {
		rendered, sampleData, err := svc.Preview(ctx, domain.TemplateKeyOutboxShorts, "[{{.MemberName}}] 테스트")
		require.NoError(t, err)
		assert.Contains(t, rendered, "사쿠라 미코")
		assert.NotNil(t, sampleData)
	})

	t.Run("parse error in preview", func(t *testing.T) {
		_, _, err := svc.Preview(ctx, domain.TemplateKeyOutboxShorts, "{{.MemberName")
		assert.ErrorIs(t, err, template.ErrTemplateParseError)
	})

	t.Run("invalid key", func(t *testing.T) {
		_, _, err := svc.Preview(ctx, domain.TemplateKey("INVALID"), "body")
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_GetRevisions(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	for i := range 3 {
		_, _ = svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v"+string(rune('0'+i)))
	}

	revisions, err := svc.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, revisions)
}

func TestAdminService_GetRevisionByID(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	svc := template.NewAdminService(repo, renderer, logger)
	ctx := context.Background()

	_, _ = svc.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] updated")

	revisions, _ := svc.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	if len(revisions) > 0 {
		rev, err := svc.GetRevisionByID(ctx, revisions[0].ID)
		require.NoError(t, err)
		assert.NotNil(t, rev)
	}

	t.Run("not found", func(t *testing.T) {
		_, err := svc.GetRevisionByID(ctx, 99999)
		assert.ErrorIs(t, err, template.ErrRevisionNotFound)
	})
}
