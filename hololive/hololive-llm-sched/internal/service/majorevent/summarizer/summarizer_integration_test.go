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

package summarizer

import (
	"fmt"
	json "github.com/park285/shared-go/pkg/json"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func feb2026Events() []domain.MajorEvent {
	date := func(y, m, d int) *time.Time {
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
		return new(t)
	}

	return []domain.MajorEvent{
		{
			ID: 930, Title: "hololive DEV_IS NEW WAVE POP UP STORE in OIOI",
			Type: domain.MajorEventTypeNews, Link: "https://hololivepro.com/news/930",
			Members:        []string{"火威青", "儒烏風亭らでん", "一条莉々華", "虎金妃笑虎", "綺々羅々ヴィヴィ", "春先のどか", "輪堂千速", "水宮枢", "響咲リオナ"},
			EventStartDate: date(2026, 2, 6), EventEndDate: date(2026, 2, 14),
		},
		{
			ID: 9215, Title: "東京駅と成田空港、2つのお店を巡ってオリジナルポストカードをもらおう！",
			Type: domain.MajorEventTypeNews, Link: "https://hololivepro.com/news/9215",
			EventStartDate: date(2026, 2, 12), EventEndDate: date(2026, 3, 8),
		},
		{
			ID: 13590, Title: "Hoshimachi Suisei Live \"SuperNova: REBOOT\"",
			Type: domain.MajorEventTypeEvent, Link: "https://hololivepro.com/events/13590",
			Members:        []string{"星街すいせい"},
			EventStartDate: date(2026, 2, 21), EventEndDate: date(2026, 2, 21),
		},
		{
			ID: 8, Title: "秘密結社holoX Live 2026「First MISSION」",
			Type: domain.MajorEventTypeEvent, Link: "https://hololivepro.com/events/8",
			Members:        []string{"ラプラス", "鷹嶺ルイ", "博衣こより", "沙花叉クロヱ"},
			EventStartDate: date(2026, 2, 25), EventEndDate: date(2026, 2, 28),
		},
	}
}

func mar2026Events() []domain.MajorEvent {
	date := func(y, m, d int) *time.Time {
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
		return new(t)
	}

	return []domain.MajorEvent{
		{
			ID: 6, Title: "hololive SUPER EXPO 2026 Supported By BANDAI & hololive 7th fes. Ridin' on Dreams Supported By LAWSON",
			Type: domain.MajorEventTypeEvent, Link: "https://hololivepro.com/events/6",
			Members: []string{
				"星街すいせい", "さくらみこ", "白上フブキ", "大空スバル", "百鬼あやめ", "兎田ぺこら", "宝鐘マリン",
				"天音かなた", "角巻わため", "常闇トワ", "獅白ぼたん", "雪花ラミィ", "博衣こより", "風真いろは",
				"ラプラス", "鷹嶺ルイ", "沙花叉クロヱ", "Calli", "Kiara", "Ina", "IRyS", "Kronii", "Baelz",
				"FUWAMOCO", "Shiori", "Bijou", "Nerissa", "Elizabeth", "Gigi", "Cecilia", "Raora",
			},
			EventStartDate: date(2026, 3, 5), EventEndDate: date(2026, 3, 7),
		},
		{
			ID: 931, Title: "hololive Dreams (ホロドリ) 全世界同時リリース",
			Type: domain.MajorEventTypeNews, Link: "https://hololivepro.com/news/931",
			EventStartDate: date(2026, 3, 5), EventEndDate: date(2026, 3, 5),
		},
		{
			ID: 932, Title: "常闇トワ アニメタイアップ全国流通シングルCDリリース",
			Type: domain.MajorEventTypeNews, Link: "https://hololivepro.com/news/932",
			Members:        []string{"常闇トワ"},
			EventStartDate: date(2026, 3, 24), EventEndDate: date(2026, 3, 24),
		},
		{
			ID: 9, Title: "Takanashi Kiara / Ninomae Ina'nis 1st Concert \"Drawn to Dawn\"",
			Type: domain.MajorEventTypeEvent, Link: "https://hololivepro.com/events/9",
			Members:        []string{"Kiara", "Ina"},
			EventStartDate: date(2026, 3, 27), EventEndDate: date(2026, 3, 28),
		},
		{
			ID: 945, Title: "VTuberグループ「ホロライブ」の公式ファンクラブを開設",
			Type: domain.MajorEventTypeNews, Link: "https://hololivepro.com/news/945",
			EventStartDate: date(2026, 3, 30), EventEndDate: date(2026, 3, 30),
		},
	}
}

func TestIntegration_BuildUserPrompt_Output(t *testing.T) {
	// API 호출 없이 프롬프트 생성 검증 (INTEGRATION_TEST 불필요)
	events := feb2026Events()
	prompt := buildUserPrompt(events, SummaryTypeWeekly, "2026-02-21")

	t.Logf("\n=== Weekly User Prompt ===\n%s\n=== END ===", prompt)

	if !strings.Contains(prompt, "4건") {
		t.Errorf("이벤트 수(4건)가 프롬프트에 없음")
	}
	if !strings.Contains(prompt, "2026-02-21") {
		t.Error("periodKey가 프롬프트에 없음")
	}

	// JSON 배열 형식 검증
	var promptEvents []eventForPrompt
	// 프롬프트에서 JSON 부분 추출
	idx := strings.Index(prompt, "[")
	if idx == -1 {
		t.Fatal("프롬프트에 JSON 배열이 없음")
	}
	jsonPart := prompt[idx:]
	if err := json.Unmarshal([]byte(jsonPart), &promptEvents); err != nil {
		t.Fatalf("프롬프트 JSON 파싱 실패: %v", err)
	}

	if len(promptEvents) != 4 {
		t.Errorf("expected 4 events in prompt, got %d", len(promptEvents))
	}

	// 날짜 형식 검증 (eventForPrompt.DateStr)
	for _, pe := range promptEvents {
		if pe.DateStr == "" || pe.DateStr == "TBA" {
			t.Errorf("event %q has no date", pe.Title)
		}
		if pe.Title == "" {
			t.Error("event title is empty")
		}
		fmt.Printf("  prompt event: title=%q date=%q members=%q\n", pe.Title, pe.DateStr, pe.Members)
	}
}
