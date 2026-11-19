package template

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"
	"text/template"
	"time"
)

func benchRenderer(b *testing.B) (*Renderer, *template.Template) {
	b.Helper()

	r := NewRenderer(nil, slog.New(slog.DiscardHandler))

	tmpl, err := template.New("bench").Funcs(templateFuncs).Parse("{{.Name}} 시작 {{formatNumberKR .Viewers}}")
	if err != nil {
		b.Fatalf("parse bench template: %v", err)
	}

	return r, tmpl
}

func BenchmarkParsedTemplateCacheHit(b *testing.B) {
	r, tmpl := benchRenderer(b)
	ck := cacheKey{templateKey: "bench", channelID: "ch-hit"}
	r.storeTemplateAt(ck, tmpl, time.Now())

	b.ReportAllocs()

	for b.Loop() {
		if r.cachedTemplate(ck) != tmpl {
			b.Fatal("parsed cache miss")
		}
	}
}

func BenchmarkStoreTemplateAtFullCache(b *testing.B) {
	r, tmpl := benchRenderer(b)
	base := time.Now()

	for i := range templateCacheMaxEntries {
		ck := cacheKey{templateKey: "bench", channelID: fmt.Sprintf("ch-%d", i)}
		r.storeTemplateAt(ck, tmpl, base.Add(time.Duration(i)*time.Millisecond))
	}

	b.ReportAllocs()

	i := 0

	for b.Loop() {
		ck := cacheKey{templateKey: "bench", channelID: fmt.Sprintf("new-%d", i)}
		r.storeTemplateAt(ck, tmpl, base.Add(time.Hour))

		i++
	}
}

func BenchmarkExecuteParsedTemplate(b *testing.B) {
	r, tmpl := benchRenderer(b)
	ck := cacheKey{templateKey: "bench", channelID: "ch-render"}
	r.storeTemplateAt(ck, tmpl, time.Now())

	data := struct {
		Name    string
		Viewers int
	}{Name: "soda", Viewers: 15234}

	b.ReportAllocs()

	for b.Loop() {
		var buf bytes.Buffer

		if err := tmpl.Execute(&buf, data); err != nil {
			b.Fatalf("render cached template: %v", err)
		}
	}
}
