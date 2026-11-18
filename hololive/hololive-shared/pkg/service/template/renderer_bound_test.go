package template

import (
	"fmt"
	"log/slog"
	"testing"
	"text/template"
	"time"
)

const boundTestTemplateKey = "alarm"

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

func TestStoreTemplate_EnforcesSizeBound(t *testing.T) {
	r := newBoundTestRenderer()
	tmpl := parsedTestTemplate(t)
	base := time.Now()

	total := templateCacheMaxEntries + 100
	for i := range total {
		ck := cacheKey{templateKey: boundTestTemplateKey, channelID: fmt.Sprintf("ch-%d", i)}
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
		ck := cacheKey{templateKey: boundTestTemplateKey, channelID: fmt.Sprintf("ch-%d", i)}
		r.storeTemplateAt(ck, tmpl, base.Add(time.Duration(i)*time.Millisecond))
	}

	overflow := cacheKey{templateKey: boundTestTemplateKey, channelID: "ch-overflow"}
	r.storeTemplateAt(overflow, tmpl, base.Add(time.Hour))

	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	if len(r.cache) > templateCacheMaxEntries {
		t.Fatalf("template cache exceeded cap after overflow insert: size=%d cap=%d",
			len(r.cache), templateCacheMaxEntries)
	}

	if _, ok := r.cache[cacheKey{templateKey: boundTestTemplateKey, channelID: "ch-0"}]; ok {
		t.Fatal("expected oldest entry ch-0 to be evicted")
	}

	if _, ok := r.cache[overflow]; !ok {
		t.Fatal("expected overflow entry to be cached")
	}
}

func TestStoreTemplate_KeepsCachedTemplateUsable(t *testing.T) {
	r := newBoundTestRenderer()
	tmpl := parsedTestTemplate(t)
	ck := cacheKey{templateKey: boundTestTemplateKey, channelID: "ch-keep"}

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
