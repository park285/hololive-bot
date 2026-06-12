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
	"fmt"
	"log/slog"
	"testing"
	"text/template"
	"time"
)

func newBoundTestRenderer() *Renderer {
	return NewRenderer(nil, slog.New(slog.DiscardHandler))
}

func parsedTestTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("bound").Parse("body")
	if err != nil {
		t.Fatalf("parse test template: %v", err)
	}
	return tmpl
}

// 키가 (templateKey, channelID) 조합이라 채널 수에 비례해 자랄 수 있으므로 size-bound 를 검증한다.
func TestStoreTemplate_EnforcesSizeBound(t *testing.T) {
	r := newBoundTestRenderer()
	tmpl := parsedTestTemplate(t)
	base := time.Now()

	total := templateCacheMaxEntries + 100
	for i := range total {
		ck := cacheKey{templateKey: "alarm", channelID: fmt.Sprintf("ch-%d", i)}
		r.storeTemplateAt(ck, tmpl, base.Add(time.Duration(i)*time.Millisecond))
	}

	r.cacheMu.RLock()
	size := len(r.cache)
	r.cacheMu.RUnlock()

	if size > templateCacheMaxEntries {
		t.Fatalf("template cache exceeded cap: size=%d cap=%d", size, templateCacheMaxEntries)
	}
}

func TestStoreTemplate_EvictsOldestWhenFull(t *testing.T) {
	r := newBoundTestRenderer()
	tmpl := parsedTestTemplate(t)
	base := time.Now()

	for i := range templateCacheMaxEntries {
		ck := cacheKey{templateKey: "alarm", channelID: fmt.Sprintf("ch-%d", i)}
		r.storeTemplateAt(ck, tmpl, base.Add(time.Duration(i)*time.Millisecond))
	}

	overflow := cacheKey{templateKey: "alarm", channelID: "ch-overflow"}
	r.storeTemplateAt(overflow, tmpl, base.Add(time.Hour))

	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	if len(r.cache) > templateCacheMaxEntries {
		t.Fatalf("template cache exceeded cap after overflow insert: size=%d cap=%d",
			len(r.cache), templateCacheMaxEntries)
	}
	if _, ok := r.cache[cacheKey{templateKey: "alarm", channelID: "ch-0"}]; ok {
		t.Fatal("expected oldest entry ch-0 to be evicted")
	}
	if _, ok := r.cache[overflow]; !ok {
		t.Fatal("expected overflow entry to be cached")
	}
}

func TestStoreTemplate_KeepsCachedTemplateUsable(t *testing.T) {
	r := newBoundTestRenderer()
	tmpl := parsedTestTemplate(t)
	ck := cacheKey{templateKey: "alarm", channelID: "ch-keep"}

	r.storeTemplateAt(ck, tmpl, time.Now())

	r.cacheMu.RLock()
	entry, ok := r.cache[ck]
	r.cacheMu.RUnlock()
	if !ok {
		t.Fatal("expected stored template to be cached")
	}
	if entry.tmpl != tmpl {
		t.Fatal("cached entry must keep the parsed template")
	}
}
