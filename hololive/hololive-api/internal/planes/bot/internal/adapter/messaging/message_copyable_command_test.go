package messaging

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func TestSeedHelpCommandsCopyRoundTrip(t *testing.T) {
	pool := dbtest.NewPool(t)
	renderer := template.NewRenderer(pool, slog.Default())

	for _, prefix := range []string{"!", "#", "*", "\\", "**", "[]", ">", "1.", "bot:"} {
		t.Run(prefix, func(t *testing.T) {
			rendered, err := renderer.Render(t.Context(), domain.TemplateKeyCmdHelp, "", map[string]any{"Prefix": prefix})
			if err != nil {
				t.Fatal(err)
			}

			var command string

			for line := range strings.SplitSeq(kakaoformat.Render(rendered), "\n") {
				if strings.HasSuffix(line, "도움말 - 도움말") {
					command = strings.TrimSuffix(line, " - 도움말")
				}
			}

			parsed := NewMessageAdapter(prefix, "").ParseMessage(&webhook.Message{Msg: command})
			if parsed == nil || parsed.Type != domain.CommandHelp {
				t.Errorf("copy command %q failed: %#v", command, parsed)
			}
		})
	}
}

func TestAmbiguousMemberExampleCopyPreservesArgument(t *testing.T) {
	pool := dbtest.NewPool(t)
	renderer := template.NewRenderer(pool, slog.Default())
	f := formatter.NewResponseFormatter("#", renderer)

	for _, name := range []string{"미코", "Star*Name", "Name_with_underscores", "A & B", "[멤버]", "Mi`ko", `A\B`} {
		t.Run(name, func(t *testing.T) {
			member := &domain.Member{Name: name}
			output := kakaoformat.Render(f.FormatAmbiguousMembers(t.Context(), []*domain.Member{member}, "라이브"))

			var command string

			for line := range strings.SplitSeq(output, "\n") {
				if example, ok := strings.CutPrefix(line, "예) "); ok {
					command = example
				}
			}

			parsed := NewMessageAdapter("#", "").ParseMessage(&webhook.Message{Msg: command})
			if parsed == nil || parsed.Type != domain.CommandLive || parsed.Params[paramMember] != member.GetDisplayName() {
				t.Errorf("copy example changed member argument: command=%q parsed=%#v", command, parsed)
			}
		})
	}
}

func TestSeedCommandPrefixesRemainCopyable(t *testing.T) {
	pool := dbtest.NewPool(t)
	renderer := template.NewRenderer(pool, slog.Default())

	for _, tc := range []struct {
		key     domain.TemplateKey
		command string
	}{
		{domain.TemplateKeyCmdMajorEventNotSub, "행사 켜기"},
		{domain.TemplateKeyCmdMajorEventStatus, "행사 켜기"},
		{domain.TemplateKeyCmdMajorEventUsage, "행사 켜기"},
		{domain.TemplateKeyCmdMemberNewsNoMembers, "알람 추가 페코라"},
		{domain.TemplateKeyCmdMemberNewsStatus, "뉴스알림 켜기"},
		{domain.TemplateKeyCmdAmbiguousMember, "라이브 미코"},
		{domain.TemplateKeyCmdAlarmList, "알람 추가 페코라"},
	} {
		t.Run(string(tc.key), func(t *testing.T) {
			for _, prefix := range []string{"!", "#", "*", "\\", "**", "[]", ">", "1.", "bot:"} {
				data := map[string]any{
					"Prefix": prefix, "Count": 0, "IsSubscribed": false,
					"Candidates": []any{}, "CommandExample": "라이브", "FirstName": "미코",
				}

				rendered, err := renderer.Render(t.Context(), tc.key, "", data)
				if err != nil {
					t.Fatal(err)
				}

				if output := kakaoformat.Render(rendered); !strings.Contains(output, prefix+tc.command) {
					t.Errorf("copyable command changed: prefix=%q output=%q", prefix, output)
				}
			}
		})
	}
}
