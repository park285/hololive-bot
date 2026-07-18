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

package formatter

import (
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
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

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`); err != nil {
		t.Fatalf("clear templates: %v", err)
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

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, body)
		VALUES ($1, NULL, $2)
		ON CONFLICT (template_key) WHERE channel_id IS NULL
		DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
	`, domain.TemplateKeyCmdMajorEventMonthlySummary, body); err != nil {
		t.Fatalf("failed to insert template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.Default())
}
