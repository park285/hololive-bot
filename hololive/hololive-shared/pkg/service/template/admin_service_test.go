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
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func setupTestService(t *testing.T) (*template.AdminService, *repository.TemplateRepository) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	templateRepo := repository.NewTemplateRepository(pool, logger)
	renderer := template.NewRenderer(pool, logger)
	service := template.NewAdminService(templateRepo, renderer, logger)

	return service, templateRepo
}

func TestAdminService_List(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := t.Context()

	templates, err := service.List(ctx, nil, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, templates)
}

func TestAdminService_GetByKey(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := t.Context()

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
	service, templateRepo := setupTestService(t)
	ctx := t.Context()

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
		existing, found, err := templateRepo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		require.NoError(t, err)
		require.True(t, found)

		oldBody := existing.Body

		_, err = service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v2")
		require.NoError(t, err)

		revisions, err := templateRepo.GetRevisions(ctx, existing.ID, 5)
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
	service, templateRepo := setupTestService(t)
	ctx := t.Context()

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

		_, found, err := templateRepo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestAdminService_Preview(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := t.Context()

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
	service, _ := setupTestService(t)
	ctx := t.Context()

	for i := range 3 {
		_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] v"+string(rune('0'+i)))
		require.NoError(t, err)
	}

	revisions, err := service.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, revisions)
}

func TestAdminService_GetRevisionByID(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := t.Context()

	_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] updated")
	require.NoError(t, err)

	revisions, err := service.GetRevisions(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)

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

func TestAdminService_SavePrunesToMaxRevisions(t *testing.T) {
	service, templateRepo := setupTestService(t)
	ctx := t.Context()

	const saveCount = 7

	bodies := make([]string, 0, saveCount)

	for i := range saveCount {
		body := fmt.Sprintf("[{{.MemberName}}] v%d", i)

		bodies = append(bodies, body)

		_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, body)
		require.NoError(t, err)
	}

	tmpl, found, err := templateRepo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, bodies[saveCount-1], tmpl.Body)

	stored, err := templateRepo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	require.Len(t, stored, 5, "prune must keep exactly maxRevisions rows")

	want := []string{bodies[5], bodies[4], bodies[3], bodies[2], bodies[1]}
	got := make([]string, 0, len(stored))

	for _, rev := range stored {
		got = append(got, rev.Body)
	}

	assert.Equal(t, want, got)
}

func TestAdminService_SaveRollsBackOnContextCancel(t *testing.T) {
	service, templateRepo := setupTestService(t)
	ctx := t.Context()

	const committed = "[{{.MemberName}}] committed"

	_, err := service.Save(ctx, domain.TemplateKeyOutboxShorts, nil, committed)
	require.NoError(t, err)

	tmpl, found, err := templateRepo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	require.True(t, found)

	before, err := templateRepo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = service.Save(canceled, domain.TemplateKeyOutboxShorts, nil, "[{{.MemberName}}] never lands")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "want context.Canceled in chain, got %v", err)

	after, found, err := templateRepo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, committed, after.Body, "failed save must not replace the body")

	afterRevisions, err := templateRepo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, afterRevisions, len(before), "failed save must not leave a revision behind")
}
