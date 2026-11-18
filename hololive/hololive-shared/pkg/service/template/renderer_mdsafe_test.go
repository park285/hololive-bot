package template

import (
	"strings"
	"testing"
	"text/template"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

func TestTemplateFuncs_MdSafe(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"제목: {{mdsafe .Title}} 끝", "제목: **{{mdsafe .Title}}** 끝"} {
		tmpl, err := template.New("mdsafe").Funcs(templateFuncs).Parse(body)
		if err != nil {
			t.Fatal(err)
		}

		for _, title := range []string{"**긴급**", "끝**", "*시작", "||숨김||", "`코드`", "[공식명]", "_원문_", "~~표기~~", "- 안내"} {
			var buf strings.Builder

			if err := tmpl.Execute(&buf, struct{ Title string }{Title: title}); err != nil {
				t.Fatal(err)
			}

			visible := kakaoformat.Render(buf.String())
			if !strings.Contains(visible, title) || !strings.HasSuffix(visible, " 끝") {
				t.Errorf("literal 경계 소실: title=%q out=%q", title, visible)
			}
		}
	}
}
