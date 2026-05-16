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

package messaging

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"gorm.io/gorm"
)

func TestFormatMemberNewsDigest_RendersTemplate(t *testing.T) {
	renderer := setupMemberNewsRenderer(t)
	formatter := NewResponseFormatter("!", renderer)

	digest := &membernewscontracts.Digest{
		Headline: "🗞️ 이번주 구독 멤버 뉴스",
		TopItems: []membernewscontracts.SummaryItem{
			{Member: "사쿠라 미코", Category: "birthday_live", Title: "생일 라이브", DateText: "2026-02-20", SourceURL: "https://hololive.hololivepro.com/news/1"},
			{Member: "후부키", Category: "event", Title: "EXPO", DateText: "2026-02-21", SourceURL: "https://hololive.hololivepro.com/news/2"},
		},
		MoreSummary: "외 3건",
	}

	output := formatter.FormatMemberNewsDigest(t.Context(), digest)
	if !strings.Contains(output, digest.Headline) {
		t.Fatalf("output should contain headline, got: %s", output)
	}

	if !strings.Contains(output, "https://hololive.hololivepro.com/news/1") {
		t.Fatalf("output should contain source link, got: %s", output)
	}

	if !strings.Contains(output, "외 3건") {
		t.Fatalf("output should contain more summary, got: %s", output)
	}
}

func TestFormatMemberNewsDigest_LocalizesCategoryLabel(t *testing.T) {
	renderer := setupMemberNewsRenderer(t)
	formatter := NewResponseFormatter("!", renderer)

	digest := &membernewscontracts.Digest{
		Headline: "🗞️ 테스트",
		TopItems: []membernewscontracts.SummaryItem{
			{Member: "호시마치 스이세이", Category: "solo_live", Title: "라이브", DateText: "2026-02-20", SourceURL: "https://hololive.hololivepro.com/news/solo"},
		},
	}

	output := formatter.FormatMemberNewsDigest(t.Context(), digest)
	if !strings.Contains(output, "솔로 라이브") {
		t.Fatalf("output should contain localized category label, got: %s", output)
	}

	if strings.Contains(output, "solo_live") {
		t.Fatalf("output should not contain raw category code, got: %s", output)
	}
}

func TestFormatMemberNewsDigest_RenderFailFallback(t *testing.T) {
	formatter := NewResponseFormatter("!", nil)
	digest := &membernewscontracts.Digest{Headline: "뉴스"}

	output := formatter.FormatMemberNewsDigest(t.Context(), digest)

	expected := ErrorMessage(ErrDisplayMemberNewsFailed)
	if output != expected {
		t.Fatalf("expected %q, got %q", expected, output)
	}
}

func setupMemberNewsRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(&domain.NotificationTemplate{}); err != nil {
		t.Fatalf("failed to migrate notification_templates: %v", err)
	}

	body := `{{.Headline}}
{{range $index, $item := .TopItems}}{{if gt $index 0}}\n{{end}}{{$item.Member}} {{$item.Category}} {{$item.Title}} {{$item.SourceURL}}{{end}}
{{if .MoreSummary}}{{.MoreSummary}}{{end}}`
	if err := db.Create(&domain.NotificationTemplate{
		TemplateKey: domain.TemplateKeyCmdMemberNewsDigest,
		Body:        body,
	}).Error; err != nil {
		t.Fatalf("failed to insert template: %v", err)
	}

	return serviceTemplate.NewRenderer(db, slog.Default())
}
