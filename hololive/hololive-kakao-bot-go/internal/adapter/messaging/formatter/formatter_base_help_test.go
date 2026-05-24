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

package formatter

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFormatterTestRenderer(t *testing.T, templates map[domain.TemplateKey]string) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.NotificationTemplate{}))

	for key, body := range templates {
		err := db.Create(&domain.NotificationTemplate{
			TemplateKey: key,
			Body:        body,
		}).Error
		require.NoError(t, err)
	}

	logger := slog.New(slog.DiscardHandler)

	return serviceTemplate.NewRenderer(db, logger)
}

func TestSplitTemplateInstruction(t *testing.T) {
	t.Parallel()

	t.Run("blank string", func(t *testing.T) {
		t.Parallel()

		instruction, body := splitTemplateInstruction("\n\r\n")
		assert.Empty(t, instruction)
		assert.Empty(t, body)
	})

	t.Run("single line only", func(t *testing.T) {
		t.Parallel()

		instruction, body := splitTemplateInstruction("사용법 안내만")
		assert.Equal(t, "사용법 안내만", instruction)
		assert.Empty(t, body)
	})

	t.Run("instruction and body", func(t *testing.T) {
		t.Parallel()

		instruction, body := splitTemplateInstruction("\n\r\n안내\r\n\r\n본문 첫줄\n본문 둘째줄")
		assert.Equal(t, "안내", instruction)
		assert.Equal(t, "본문 첫줄\n본문 둘째줄", body)
	})
}

func TestNewResponseFormatterAndPrefix(t *testing.T) {
	t.Parallel()

	t.Run("default prefix when blank", func(t *testing.T) {
		t.Parallel()

		formatter := NewResponseFormatter("   ", nil)
		require.NotNil(t, formatter)
		assert.Equal(t, "!", formatter.Prefix())
	})

	t.Run("trimmed custom prefix", func(t *testing.T) {
		t.Parallel()

		formatter := NewResponseFormatter("  #  ", nil)
		assert.Equal(t, "#", formatter.Prefix())
	})

	t.Run("nil formatter", func(t *testing.T) {
		t.Parallel()

		var formatter *ResponseFormatter
		assert.Equal(t, "!", formatter.Prefix())
	})
}

func TestResponseFormatterRender(t *testing.T) {
	t.Parallel()

	t.Run("render success with trailing newline trim", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdHelp: "Hello {{.Name}}\n",
		})
		formatter := NewResponseFormatter("!", renderer)

		got, err := formatter.render(t.Context(), domain.TemplateKeyCmdHelp, map[string]string{"Name": "Kapu"})
		require.NoError(t, err)
		assert.Equal(t, "Hello Kapu", got)
	})

	t.Run("render missing template", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{})
		formatter := NewResponseFormatter("!", renderer)

		_, err := formatter.render(t.Context(), domain.TemplateKeyCmdHelp, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "render template")
	})
}

func TestFormatHelp(t *testing.T) {
	t.Parallel()

	t.Run("template with instruction and body applies see-more padding", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdHelp: "도움말 안내\n사용 가능한 명령어 목록",
		})
		formatter := NewResponseFormatter("!", renderer)

		got := formatter.FormatHelp(t.Context())
		assert.True(t, strings.HasPrefix(got, "도움말 안내"))
		assert.Contains(t, got, "\n사용 가능한 명령어 목록")
		assert.Equal(t, util.KakaoSeeMorePadding, strings.Count(got, util.KakaoZeroWidthSpace))
	})

	t.Run("single line template returns original render", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdHelp: "한 줄 도움말",
		})
		formatter := NewResponseFormatter("!", renderer)

		got := formatter.FormatHelp(t.Context())
		assert.Equal(t, "한 줄 도움말", got)
	})

	t.Run("render failure returns fallback error message", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{})
		formatter := NewResponseFormatter("!", renderer)

		got := formatter.FormatHelp(t.Context())
		assert.Equal(t, msging.ErrorMessage(msging.ErrDisplayHelpFailed), got)
	})
}

func TestFormatErrorAndMemberNotFound(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	assert.Equal(t, msging.ErrorMessage("테스트 오류"), formatter.FormatError("테스트 오류"))
	assert.Equal(t, msging.ErrorMessage("'후부키' 멤버를 찾을 수 없습니다."), formatter.MemberNotFound("후부키"))
}
