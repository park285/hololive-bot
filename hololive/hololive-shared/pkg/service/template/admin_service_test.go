// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	templates, err := service.List(ctx, nil, nil)
	require.NoError(t, err)
	assert.Len(t, templates, 1)
}

func TestAdminService_GetByKey(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	t.Run("existing key", func(t *testing.T) {
		defaultTmpl, overrides, err := service.GetByKey(ctx, domain.TemplateKeyOutboxShorts)
		require.NoError(t, err)
		require.NotNil(t, defaultTmpl)
		assert.Empty(t, overrides)
	})

	t.Run("non-existing key", func(t *testing.T) {
		_, _, err := service.GetByKey(ctx, domain.TemplateKey("INVALID"))
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_Save(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	t.Run("valid template update", func(t *testing.T) {
		tmpl, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] 수정됨")
		require.NoError(t, err)
		assert.Contains(t, tmpl.Body, "수정됨")
	})

	t.Run("parse error", func(t *testing.T) {
		_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "{{.MemberName")
		assert.ErrorIs(t, err, template.ErrTemplateParseError)
	})

	t.Run("render error - invalid field", func(t *testing.T) {
		_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "{{.InvalidField}}")
		assert.ErrorIs(t, err, template.ErrTemplateRenderError)
	})

	t.Run("creates revision on update", func(t *testing.T) {
		existing, _ := repository.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		oldBody := existing.Body

		_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v2")
		require.NoError(t, err)

		revisions, err := repository.GetRevisions(ctx, existing.ID, 5)
		require.NoError(t, err)
		assert.NotEmpty(t, revisions)
		assert.Equal(t, oldBody, revisions[0].Body)
	})

	t.Run("invalid template key", func(t *testing.T) {
		_, err := service.Save(ctx, domain.TemplateKey("INVALID"), nil, "body")
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_DeleteOverride(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	channelID := "room_123"
	_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, &channelID, "[커스텀]")
	require.NoError(t, err)

	t.Run("channel_id required", func(t *testing.T) {
		err := service.DeleteOverride(ctx, domain.TemplateKeyOutboxShorts, "")
		assert.ErrorIs(t, err, template.ErrChannelIDRequired)
	})

	t.Run("successful delete", func(t *testing.T) {
		err := service.DeleteOverride(ctx, domain.TemplateKeyOutboxShorts, channelID)
		require.NoError(t, err)

		found, _ := repository.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
		assert.Nil(t, found)
	})
}

func TestAdminService_Preview(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	t.Run("successful preview", func(t *testing.T) {
		rendered, sampleData, err := service.Preview(ctx, domain.TemplateKeyOutboxShorts, "[{{.MemberName}}] 테스트")
		require.NoError(t, err)
		assert.Contains(t, rendered, "사쿠라 미코")
		assert.NotNil(t, sampleData)
	})

	t.Run("parse error in preview", func(t *testing.T) {
		_, _, err := service.Preview(ctx, domain.TemplateKeyOutboxShorts, "{{.MemberName")
		assert.ErrorIs(t, err, template.ErrTemplateParseError)
	})

	t.Run("invalid key", func(t *testing.T) {
		_, _, err := service.Preview(ctx, domain.TemplateKey("INVALID"), "body")
		assert.ErrorIs(t, err, template.ErrTemplateKeyNotFound)
	})
}

func TestAdminService_GetRevisions(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	for i := range 3 {
		_, _ = service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v"+string(rune('0'+i)))
	}

	revisions, err := service.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, revisions)
}

func TestAdminService_GetRevisionByID(t *testing.T) {
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	repository := repository.NewTemplateRepository(db, logger)
	renderer := template.NewRenderer(db, logger)
	service := template.NewAdminService(repository, renderer, logger)
	ctx := context.Background()

	_, _ = service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] updated")

	revisions, _ := service.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	if len(revisions) > 0 {
		rev, err := service.GetRevisionByID(ctx, revisions[0].ID)
		require.NoError(t, err)
		assert.NotNil(t, rev)
	}

	t.Run("not found", func(t *testing.T) {
		_, err := service.GetRevisionByID(ctx, 99999)
		assert.ErrorIs(t, err, template.ErrRevisionNotFound)
	})
}
