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

package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func setupFormatterRenderer(t *testing.T, key domain.TemplateKey, body string) *template.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&domain.NotificationTemplate{})
	require.NoError(t, err)

	err = db.Create(&domain.NotificationTemplate{
		TemplateKey: key,
		Body:        body,
	}).Error
	require.NoError(t, err)

	return template.NewRenderer(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNewLLMSchedulerFormatter_Defaults(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("   ", nil, nil)
	require.NotNil(t, formatter)

	assert.Equal(t, "!", formatter.prefix)
	assert.Nil(t, formatter.renderer)
	require.NotNil(t, formatter.logger)
}

func TestNewLLMSchedulerFormatter_UsesProvidedValues(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "안내\n본문")

	formatter := newLLMSchedulerFormatter("?", renderer, logger)
	require.NotNil(t, formatter)

	assert.Equal(t, "?", formatter.prefix)
	assert.Equal(t, renderer, formatter.renderer)
	assert.Equal(t, logger, formatter.logger)
}

func TestErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "❌ 실패", errorMessage("실패"))
}

func TestSplitTemplateInstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		rendered        string
		wantInstruction string
		wantBody        string
	}{
		{
			name:            "empty",
			rendered:        "\n\n",
			wantInstruction: "",
			wantBody:        "",
		},
		{
			name:            "instruction only",
			rendered:        "안내문",
			wantInstruction: "안내문",
			wantBody:        "",
		},
		{
			name:            "instruction and body",
			rendered:        "\r\n 자세히 보기 \r\n\r\n본문 줄1\n본문 줄2",
			wantInstruction: "자세히 보기",
			wantBody:        "본문 줄1\n본문 줄2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruction, body := splitTemplateInstruction(tt.rendered)
			assert.Equal(t, tt.wantInstruction, instruction)
			assert.Equal(t, tt.wantBody, body)
		})
	}
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
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)))

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, map[string]string{"name": "미코"})
		require.NoError(t, err)
		assert.Equal(t, "안내\n본문: 미코", rendered)
	})

	t.Run("template execute error", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "{{.MissingField}}")
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)))

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, struct{}{})
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "render template")
	})

	t.Run("template not found", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, "안내")
		formatter := newLLMSchedulerFormatter("!", renderer, slog.New(slog.NewTextHandler(io.Discard, nil)))

		rendered, err := formatter.render(context.Background(), domain.TemplateKeyCmdMajorEventWeeklySummary, nil)
		require.Error(t, err)
		assert.Empty(t, rendered)
		assert.Contains(t, err.Error(), "get template")
	})
}
