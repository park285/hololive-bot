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

package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func setupMemberNewsStore(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, store.Load(t.Context()))
	return store
}

func setupFormatterRenderer(t *testing.T, key domain.TemplateKey, body string) *template.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, body)
		VALUES ($1, NULL, $2)
		ON CONFLICT (template_key) WHERE channel_id IS NULL
		DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
	`, key, body)
	require.NoError(t, err)

	return template.NewRenderer(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func setupFormatterRendererMulti(t *testing.T, bodies map[domain.TemplateKey]string) *template.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`)
	require.NoError(t, err)
	for key, body := range bodies {
		_, err = pool.Exec(t.Context(), `
			INSERT INTO notification_templates(template_key, channel_id, body)
			VALUES ($1, NULL, $2)
			ON CONFLICT (template_key) WHERE channel_id IS NULL
			DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
		`, key, body)
		require.NoError(t, err)
	}

	return template.NewRenderer(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNewLLMSchedulerFormatter_Defaults(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("   ", nil, nil, false)
	require.NotNil(t, formatter)

	assert.Equal(t, "!", formatter.prefix)
	assert.Nil(t, formatter.renderer)
	require.NotNil(t, formatter.logger)
}

func TestNewLLMSchedulerFormatter_UsesProvidedValues(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "안내\n본문")

	formatter := newLLMSchedulerFormatter("?", renderer, logger, false)
	require.NotNil(t, formatter)

	assert.Equal(t, "?", formatter.prefix)
	assert.Equal(t, renderer, formatter.renderer)
	assert.Equal(t, logger, formatter.logger)
}

func TestLLMSchedulerFormatterRender(t *testing.T) {
	t.Parallel()

	t.Run("nil formatter", func(t *testing.T) {
		t.Parallel()

		var formatter *llmSchedulerFormatter
		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, nil)
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "template renderer not configured")
	})

	t.Run("nil renderer", func(t *testing.T) {
		t.Parallel()

		formatter := &llmSchedulerFormatter{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, nil)
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "template renderer not configured")
	})

	t.Run("success trims trailing newline", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "안내\n본문: {{.name}}\n")
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)), false)

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, map[string]string{"name": "미코"})
		require.NoError(t, err)
		assert.Equal(t, "안내\n본문: 미코", rendered)
	})

	t.Run("template execute error", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "{{.MissingField}}")
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)), false)

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, struct{}{})
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "render template")
	})

	t.Run("template not found", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "안내")
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)), false)

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMajorEventWeeklySummary, nil)
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "get template")
	})
}
