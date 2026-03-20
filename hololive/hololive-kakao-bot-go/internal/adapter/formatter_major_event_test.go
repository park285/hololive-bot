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

package adapter

import (
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"gorm.io/gorm"
)

func TestFormatMajorEventDates(t *testing.T) {
	tests := []struct {
		name     string
		dates    []time.Time
		contains string
	}{
		{
			name:     "empty dates",
			dates:    []time.Time{},
			contains: "TBA",
		},
		{
			name:     "single date",
			dates:    []time.Time{time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)},
			contains: "2026년 3월 6일",
		},
		{
			name: "multiple dates (range)",
			dates: []time.Time{
				time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC),
				time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC),
			},
			contains: "~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMajorEventDates(tt.dates)
			if !containsString(result, tt.contains) {
				t.Errorf("formatMajorEventDates() = %q, should contain %q", result, tt.contains)
			}
		})
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func TestFormatMajorEventDatesFromDB(t *testing.T) {
	tests := []struct {
		name     string
		start    *time.Time
		end      *time.Time
		contains string
	}{
		{
			name:     "nil start date",
			start:    nil,
			end:      nil,
			contains: "TBA",
		},
		{
			name:     "single date (end nil)",
			start:    new(time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)),
			end:      nil,
			contains: "2026년 3월 6일",
		},
		{
			name:     "same start and end",
			start:    new(time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)),
			end:      new(time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)),
			contains: "2026년 3월 6일",
		},
		{
			name:     "date range",
			start:    new(time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)),
			end:      new(time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)),
			contains: "~",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMajorEventDatesFromDB(tt.start, tt.end)
			if !containsString(result, tt.contains) {
				t.Errorf("formatMajorEventDatesFromDB() = %q, should contain %q", result, tt.contains)
			}
		})
	}
}

//go:fix inline
func ptrTime(t time.Time) *time.Time {
	return new(t)
}

func TestFormatMajorEventMonthlySummary_NoDuplicateListWhenLLMSummaryExists(t *testing.T) {
	renderer := setupMajorEventRenderer(t)
	formatter := NewResponseFormatter("!", renderer)

	start := time.Date(2026, time.February, 20, 0, 0, 0, 0, time.UTC)
	events := []domain.MajorEvent{
		{
			Title:          "Hoshimachi Suisei Live “SuperNova: REBOOT”",
			EventStartDate: &start,
			EventEndDate:   &start,
			Members:        []string{"星街すいせい"},
			Link:           "https://hololive.hololivepro.com/events/supernova-reboot/",
		},
	}

	llmSummary := `2/20(금) Hoshimachi Suisei Live “SuperNova: REBOOT” (星街すいせい)
- 솔로 라이브 개최
https://hololive.hololivepro.com/events/supernova-reboot/`

	output := formatter.FormatMajorEventMonthlySummary(t.Context(), events, llmSummary)

	if !containsString(output, llmSummary) {
		t.Fatalf("output should contain llm summary, got: %s", output)
	}

	if containsString(output, "1. Hoshimachi Suisei Live") {
		t.Fatalf("output should not contain fallback event list when llm summary exists, got: %s", output)
	}
}

func setupMajorEventRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(&domain.NotificationTemplate{}); err != nil {
		t.Fatalf("failed to migrate notification_templates: %v", err)
	}

	body := `📅 이번 달 행사 요약 ({{.Count}}개)
{{- if .LLMSummary}}

{{.LLMSummary}}

---
{{- end}}
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   👥 {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   🔗 {{$event.Link}}
{{- end}}
{{- end}}`

	if err := db.Create(&domain.NotificationTemplate{
		TemplateKey: domain.TemplateKeyCmdMajorEventMonthlySummary,
		Body:        body,
	}).Error; err != nil {
		t.Fatalf("failed to insert template: %v", err)
	}

	return serviceTemplate.NewRenderer(db, slog.Default())
}
