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
	"context"
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

func BenchmarkGetTemplateCacheHit(b *testing.B) {
	r, tmpl := benchRenderer(b)
	ck := cacheKey{templateKey: "bench", channelID: "ch-hit"}
	r.storeTemplateAt(ck, tmpl, time.Now())
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.getTemplate(ctx, "bench", "ch-hit"); err != nil {
			b.Fatalf("getTemplate cache hit: %v", err)
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

func BenchmarkRenderCachedTemplate(b *testing.B) {
	r, tmpl := benchRenderer(b)
	ck := cacheKey{templateKey: "bench", channelID: "ch-render"}
	r.storeTemplateAt(ck, tmpl, time.Now())
	ctx := context.Background()
	data := struct {
		Name    string
		Viewers int
	}{Name: "soda", Viewers: 15234}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Render(ctx, "bench", "ch-render", data); err != nil {
			b.Fatalf("render cached template: %v", err)
		}
	}
}
