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

	"github.com/kapu/hololive-shared/pkg/util"
)

func TestTemplateFuncs_MdSafe(t *testing.T) {
	t.Parallel()

	tmpl, err := template.New("mdsafe").Funcs(templateFuncs).Parse("제목: {{mdsafe .Title}}")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	data := struct {
		Title string
	}{
		Title: "**긴급** [공지](https://example.com)",
	}

	var buf strings.Builder

	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "**") || strings.Contains(got, "](") {
		t.Errorf("markdown 마커가 무력화되지 않음: %q", got)
	}

	if want := "제목: " + util.MarkdownNeutralize(data.Title); got != want {
		t.Errorf("mdsafe 출력 = %q, want %q", got, want)
	}

	if stripped := strings.ReplaceAll(got, util.KakaoZeroWidthSpace, ""); stripped != "제목: "+data.Title {
		t.Errorf("가시 문자 변형됨: %q", stripped)
	}
}
