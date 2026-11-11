package formatter

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

func TestRenderSelectsTemplateVariantFromReplyFormat(t *testing.T) {
	pool := dbtest.NewPool(t)

	_, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body = format_variant || ' {{.MemberName}}'
WHERE template_key = $1 AND channel_id IS NULL`, domain.TemplateKeyCmdMemberNotFound)
	require.NoError(t, err)

	f := NewResponseFormatter("!", serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler)))

	require.Equal(t, "plain 미코", f.MemberNotFound(t.Context(), "미코"))
	require.Equal(t, "plain 미코", f.MemberNotFound(WithReplyFormat(t.Context(), domain.TemplateFormatPlain), "미코"))
	require.Equal(t, "richtext 미코", f.MemberNotFound(WithReplyFormat(t.Context(), domain.TemplateFormatRichText), "미코"))
}

func TestBroadcastHistoryEntryHeadFollowsReplyFormat(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)
	entries := []BroadcastHistoryEntry{
		{MemberName: testDisplayName, TypeLabel: "게임", Time: time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)},
		{MemberName: testDisplayName, TypeLabel: "노래", Time: time.Date(2026, time.July, 6, 12, 0, 0, 0, time.UTC)},
	}

	plain := f.BroadcastHistory(WithReplyFormat(t.Context(), domain.TemplateFormatPlain), BroadcastHistoryFilter{}, entries)
	richText := f.BroadcastHistory(WithReplyFormat(t.Context(), domain.TemplateFormatRichText), BroadcastHistoryFilter{}, entries)

	require.True(t, strings.HasPrefix(plain, "방송 이력 2건\n"), plain)
	require.Contains(t, plain, "\n1 · [게임] ")
	require.Contains(t, plain, "\n2 · [노래] ")
	require.NotContains(t, plain, "**")

	require.True(t, strings.HasPrefix(richText, "**방송 이력 2건**\n"), richText)
	require.Contains(t, richText, "\n**[게임] ")
	require.NotContains(t, richText, "1 · ")

	for _, message := range []string{plain, richText} {
		require.Equal(t, 1, strings.Count(message, "──────────"), message)
	}
}
