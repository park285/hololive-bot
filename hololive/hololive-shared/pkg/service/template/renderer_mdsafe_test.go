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
			// 의도한 바깥 강조와 별개로 입력의 구문 문자·내용·뒤 문장은 그대로 보여야 한다.
			if !strings.Contains(visible, title) || !strings.HasSuffix(visible, " 끝") {
				t.Errorf("literal 경계 소실: title=%q out=%q", title, visible)
			}
		}
	}
}
