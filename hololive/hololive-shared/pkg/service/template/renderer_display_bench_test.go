package template

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func BenchmarkTemplateTruncate(b *testing.B) {
	for _, tc := range []struct{ name, input string }{
		{"ascii_short", "hello world"},
		{"mixed_short", "한글 日本語 😀 오늘의 방송 제목"},
		{"mixed_truncate", strings.Repeat("한글😀", 40)},
		{"long_title", strings.Repeat("긴 제목 ", 1000)},
	} {
		for _, version := range []struct {
			name string
			fn   func(int, string) string
		}{
			{"previous", legacyTemplateTruncate},
			{"optimized", truncateTemplateText},
		} {
			b.Run(tc.name+"/"+version.name, func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					version.fn(64, tc.input)
				}
			})
		}
	}
}

func BenchmarkDisplayLineTemplate(b *testing.B) {
	pair := loadDisplayLineSeeds(b)[domain.TemplateKeyCmdLiveStreams]
	previousFuncs := legacyTemplateFunctions()

	for _, count := range []int{1, 100} {
		type entry struct{ ChannelName, Title, URL string }

		entries := make([]entry, count)

		for i := range entries {
			entries[i] = entry{"채널 이름", "Karaoke 오늘도 노래합니다 #노래 방송과 함께하는 라이브", fmt.Sprintf("https://youtu.be/example%04d", i)}
		}

		data := struct {
			Count   int
			Streams []entry
		}{count, entries}
		reference := renderOptimizationTemplate(b, pair.oldBody, previousFuncs, data)

		if got := renderOptimizationTemplate(b, pair.newBody, templateFuncs, data); got != reference {
			b.Fatal("optimized template changed display")
		}

		for _, version := range []struct {
			name, body string
			funcs      texttemplate.FuncMap
		}{
			{"previous", pair.oldBody, previousFuncs},
			{"optimized", pair.newBody, templateFuncs},
		} {
			tmpl := texttemplate.Must(texttemplate.New("bench").Funcs(version.funcs).Parse(version.body))

			for _, phase := range []string{"template", "combined"} {
				b.Run(fmt.Sprintf("%d/%s/%s", count, version.name, phase), func(b *testing.B) {
					b.ReportAllocs()

					for b.Loop() {
						var buf bytes.Buffer

						if err := tmpl.Execute(&buf, data); err != nil {
							b.Fatal(err)
						}

						if phase == "combined" {
							kakaoformat.Render(buf.String())
						}
					}
				})
			}
		}
	}
}
