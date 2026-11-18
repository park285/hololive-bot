package formatter

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

const renderFailureMessage = "❌ 명령 처리 중 오류가 발생했습니다."

func setupFormatterTestStore(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	require.NoError(t, store.Load(t.Context()))

	return store
}

func setupFormatterTestRenderer(t *testing.T, templates map[domain.TemplateKey]string) *serviceTemplate.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`)
	require.NoError(t, err)

	for key, body := range templates {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO notification_templates(template_key, channel_id, format_variant, body)
			SELECT $1, NULL, variant, $2 FROM unnest(ARRAY['plain', 'richtext']) AS variant
			ON CONFLICT (template_key, format_variant) WHERE channel_id IS NULL
			DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
		`, key, body)
		require.NoError(t, err)
	}

	logger := slog.New(slog.DiscardHandler)

	return serviceTemplate.NewRenderer(pool, logger)
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

	t.Run("template with instruction and body returns full render", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdHelp: "도움말 안내\n사용 가능한 명령어 목록",
		})
		formatter := NewResponseFormatter("!", renderer)

		got := formatter.FormatHelp(t.Context())
		assert.True(t, strings.HasPrefix(got, "도움말 안내"))
		assert.Contains(t, got, "\n사용 가능한 명령어 목록")
		assert.NotContains(t, got, "\u200b")
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
		formatter := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))

		got := formatter.FormatHelp(t.Context())
		assert.Equal(t, renderFailureMessage, got)
	})
}

func TestMemberNotFound(t *testing.T) {
	t.Parallel()

	nilRenderer := NewResponseFormatter("!", nil, WithMessageStrings(setupFormatterTestStore(t)))
	assert.Equal(t, renderFailureMessage, nilRenderer.MemberNotFound(t.Context(), "후부키"))

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberNotFound: "❌ '{{.MemberName}}' 멤버를 찾을 수 없습니다.",
	})
	memberFormatter := NewResponseFormatter("!", renderer)
	assert.Equal(t, "❌ '후부키' 멤버를 찾을 수 없습니다.", memberFormatter.MemberNotFound(t.Context(), "후부키"))
}

func TestGraduatedWarningPreservesLiteralAndProfileBoundary(t *testing.T) {
	pool := dbtest.NewPool(t)

	const warning = "졸업 ||상태|| **문자**"

	_, err := pool.Exec(t.Context(), `UPDATE message_strings SET value = $1 WHERE namespace = 'notify' AND key = 'graduated_member_warning'`, warning+"\n\n")
	require.NoError(t, err)

	store := messagestrings.NewStore(pool, slog.New(slog.DiscardHandler))
	require.NoError(t, store.Load(t.Context()))

	f := NewResponseFormatter("!", nil, WithMessageStrings(store))

	const profile = "프로필 본문"

	for _, format := range []domain.TemplateFormat{domain.TemplateFormatPlain, domain.TemplateFormatRichText} {
		message := f.GraduatedMemberWarning(WithReplyFormat(t.Context(), format)) + profile
		visible := kakaoformat.Render(message)
		require.Contains(t, visible, warning, format)
		require.Contains(t, visible, "\n\n"+profile, format)
		require.Equal(t, format == domain.TemplateFormatRichText, strings.HasPrefix(message, "**"), format)
	}
}
