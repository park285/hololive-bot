package majorevent

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDateExtractor_Extract(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		expectedCount int
		expectedDates []string
	}{
		{
			name:          "single japanese date",
			html:          "<p>2026年1月10日（土）</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-01-10"},
		},
		{
			name:          "multi-day event with full dates",
			html:          "<p>2026年3月6日（金） 2026年3月7日（土） 2026年3月8日（日）</p>",
			expectedCount: 3,
			expectedDates: []string{"2026-03-06", "2026-03-07", "2026-03-08"},
		},
		{
			name:          "slash format",
			html:          "<p>2026/3/6</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-03-06"},
		},
		{
			name:          "hyphen format",
			html:          "<p>2026-03-06</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-03-06"},
		},
		{
			name:          "dot format",
			html:          "<p>2026.3.6</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-03-06"},
		},
		{
			name:          "dot format with whitespace",
			html:          "<p>2026. 3.6</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-03-06"},
		},
		{
			name:          "no dates",
			html:          "<p>TBA</p>",
			expectedCount: 0,
		},
		{
			name:          "full HTML content",
			html:          "<h6>開催日時</h6><p>2026年2月21日（土）22日（日）</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-02-21"},
		},
		{
			name:          "duplicate dates",
			html:          "<p>2026年1月10日 開催日: 2026年1月10日</p>",
			expectedCount: 1,
			expectedDates: []string{"2026-01-10"},
		},
		{
			name:          "mixed formats",
			html:          "<p>2026年1月10日 と 2026/2/15</p>",
			expectedCount: 2,
			expectedDates: []string{"2026-01-10", "2026-02-15"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.Extract(tt.html)
			if len(dates) != tt.expectedCount {
				t.Errorf("got %d dates, want %d", len(dates), tt.expectedCount)
			}

			for i, expected := range tt.expectedDates {
				if i >= len(dates) {
					break
				}
				got := dates[i].Format("2006-01-02")
				if got != expected {
					t.Errorf("date[%d] = %s, want %s", i, got, expected)
				}
			}
		})
	}
}

func TestDateExtractor_ExtractWithContext(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		contextYear   int
		expectedCount int
	}{
		{
			name:          "short japanese date with context",
			html:          "<p>3月6日（金）</p>",
			contextYear:   2026,
			expectedCount: 1,
		},
		{
			name:          "short date only",
			html:          "<p>3月7日（土）</p>",
			contextYear:   2026,
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.ExtractWithContext(tt.html, tt.contextYear)
			if len(dates) != tt.expectedCount {
				t.Errorf("got %d dates, want %d", len(dates), tt.expectedCount)
			}
		})
	}
}

func TestDateExtractor_parseYMD(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name    string
		year    string
		month   string
		day     string
		wantOk  bool
		wantStr string
	}{
		{"valid date", "2026", "3", "6", true, "2026-03-06"},
		{"invalid month", "2026", "13", "6", false, ""},
		{"invalid day", "2026", "3", "32", false, ""},
		{"invalid year", "abc", "3", "6", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractor.parseYMD(tt.year, tt.month, tt.day)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got.Format("2006-01-02") != tt.wantStr {
				t.Errorf("date = %s, want %s", got.Format("2006-01-02"), tt.wantStr)
			}
		})
	}
}

func TestNewDateExtractor(t *testing.T) {
	extractor := NewDateExtractor()
	if extractor == nil {
		t.Error("NewDateExtractor() returned nil")
	}
}

func TestDateExtractor_Extract_EmptyString(t *testing.T) {
	extractor := NewDateExtractor()
	dates := extractor.Extract("")
	if len(dates) != 0 {
		t.Errorf("expected 0 dates for empty string, got %d", len(dates))
	}

	_ = time.Now()
}

func TestDateExtractor_ExtractEventDates(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		expectedDates []string
	}{
		{
			name:          "event date with ticket date - should filter ticket",
			html:          `<h6>開催日時</h6><p>2027年2月21日（土）</p><h6>チケット販売</h6><p>2026年11月25日より</p>`,
			expectedDates: []string{"2027-02-21"},
		},
		{
			name:          "multi-day range pattern",
			html:          `<p>開催期間：2027年3月6日～8日</p>`,
			expectedDates: []string{"2027-03-06", "2027-03-08"},
		},
		{
			name:          "multi-day range with weekday parentheses",
			html:          `<p>開催日程：2027年3月6日（金）～8日（日）</p>`,
			expectedDates: []string{"2027-03-06", "2027-03-08"},
		},
		{
			name:          "no context keywords - cluster fallback",
			html:          `<p>2027年5月1日 2027年5月2日 2027年5月3日</p>`,
			expectedDates: []string{"2027-05-01", "2027-05-02", "2027-05-03"},
		},
		{
			name:          "empty html",
			html:          "",
			expectedDates: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.ExtractEventDates(tt.html)

			if tt.expectedDates == nil {
				if dates != nil {
					t.Errorf("expected nil, got %v", dates)
				}
				return
			}

			if len(dates) != len(tt.expectedDates) {
				t.Errorf("got %d dates, want %d: %v", len(dates), len(tt.expectedDates), formatDates(dates))
				return
			}

			for i, expected := range tt.expectedDates {
				got := dates[i].Format("2006-01-02")
				if got != expected {
					t.Errorf("date[%d] = %s, want %s", i, got, expected)
				}
			}
		})
	}
}

func TestDateExtractor_ExtractEventDates_YearRollover(t *testing.T) {
	extractor := NewDateExtractor()

	html := `<p>開催期間：2027年12月30日～1月2日</p>`
	dates := extractor.ExtractEventDates(html)

	if len(dates) != 2 {
		t.Fatalf("expected 2 dates, got %d", len(dates))
	}

	if dates[0].Format("2006-01-02") != "2027-12-30" {
		t.Errorf("start date = %s, want 2027-12-30", dates[0].Format("2006-01-02"))
	}
	if dates[1].Format("2006-01-02") != "2028-01-02" {
		t.Errorf("end date = %s, want 2028-01-02", dates[1].Format("2006-01-02"))
	}
}

func formatDates(dates []time.Time) []string {
	result := make([]string, len(dates))
	for i, d := range dates {
		result[i] = d.Format("2006-01-02")
	}
	return result
}

func TestDateExtractor_ExtractEventDates_MaxContextDistance(t *testing.T) {
	extractor := NewDateExtractor()

	// 긍정 키워드가 150자 이상 떨어져 있으면 무시되어야 함
	// 이벤트 날짜는 부정 키워드(チケット)에 더 가깝지만, 거리 상한으로 긍정이 무시됨
	padding := strings.Repeat("あ", 200)
	html := `<p>開催日時` + padding + `</p><p>チケット販売 2027年5月1日</p>`

	dates := extractor.ExtractEventDates(html)

	// 긍정 키워드가 너무 멀어서 무시되고, 부정 키워드만 근처에 있으므로
	// 클러스터 fallback으로 처리됨 (날짜가 1개뿐이므로 그대로 반환)
	if len(dates) != 1 {
		t.Errorf("expected 1 date, got %d: %v", len(dates), formatDates(dates))
	}
}

func TestDateExtractor_ExtractEventDates_TieThreshold(t *testing.T) {
	extractor := NewDateExtractor()

	// 긍정/부정 키워드가 거의 동일 거리에 있으면 중립(0) 처리
	html := `<p>開催チケット2027年6月15日</p>`

	dates := extractor.ExtractEventDates(html)

	// 중립 스코어(0)이므로 클러스터 fallback으로 처리됨
	if len(dates) != 1 {
		t.Errorf("expected 1 date, got %d: %v", len(dates), formatDates(dates))
	}
}

func TestDateExtractor_StripHTML_RemovesScriptStyle(t *testing.T) {
	extractor := NewDateExtractor()

	html := `<p>開催日時 2027年7月1日</p><script>var date = "2025-01-01";</script><style>.date{}</style>`

	dates := extractor.ExtractEventDates(html)

	if len(dates) != 1 {
		t.Errorf("expected 1 date (script content should be stripped), got %d: %v", len(dates), formatDates(dates))
		return
	}
	if dates[0].Format("2006-01-02") != "2027-07-01" {
		t.Errorf("expected 2027-07-01, got %s", dates[0].Format("2006-01-02"))
	}
}

func TestDateExtractor_ParseYMD_StrictValidation(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name   string
		year   string
		month  string
		day    string
		wantOk bool
	}{
		{"valid date", "2027", "2", "28", true},
		{"invalid feb 30", "2027", "2", "30", false},
		{"invalid feb 31", "2027", "2", "31", false},
		{"invalid apr 31", "2027", "4", "31", false},
		{"valid leap year feb 29", "2028", "2", "29", true},
		{"invalid non-leap feb 29", "2027", "2", "29", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := extractor.parseYMD(tt.year, tt.month, tt.day)
			if ok != tt.wantOk {
				t.Errorf("parseYMD(%s, %s, %s) ok = %v, want %v", tt.year, tt.month, tt.day, ok, tt.wantOk)
			}
		})
	}
}

func TestDateExtractor_ExtractEventDates_PrefersEventDatesOverArchiveDeadline(t *testing.T) {
	extractor := NewDateExtractor()

	html := `<h6>開催日時</h6><p>2027年1月17日 2027年1月18日</p><h6>アーカイブ視聴期限</h6><p>2027年2月18日</p>`
	dates := extractor.ExtractEventDates(html)

	if len(dates) == 0 {
		t.Fatalf("got 0 dates, want at least 1")
	}

	if got := dates[0].Format("2006-01-02"); got != "2027-01-17" {
		t.Errorf("first date = %s, want 2027-01-17 (event date)", got)
	}

	for _, d := range dates {
		if d.Format("2006-01-02") == "2027-02-18" {
			t.Errorf("archive deadline date should not be selected: %v", formatDates(dates))
		}
	}
}

func TestDateExtractor_ExtractEventDates_KeepPastEventDate(t *testing.T) {
	extractor := NewDateExtractor()

	now := time.Now().In(kst)
	past := now.AddDate(0, 0, -30)
	futureArchive := now.AddDate(0, 0, 30)
	html := fmt.Sprintf(
		`<h6>開催日時</h6><p>%d年%d月%d日</p><h6>アーカイブ視聴期限</h6><p>%d年%d月%d日</p>`,
		past.Year(), int(past.Month()), past.Day(),
		futureArchive.Year(), int(futureArchive.Month()), futureArchive.Day(),
	)

	dates := extractor.ExtractEventDates(html)
	if len(dates) == 0 {
		t.Fatalf("expected at least one date, got 0")
	}

	gotFirst := dates[0].Format("2006-01-02")
	wantFirst := past.Format("2006-01-02")
	if gotFirst != wantFirst {
		t.Errorf("first date = %s, want past event date %s; all=%v", gotFirst, wantFirst, formatDates(dates))
	}
}

func TestDateExtractor_ExtractEventDates_DotStyleEventDatePreferred(t *testing.T) {
	extractor := NewDateExtractor()

	html := `<h6>開催日時</h6><p>2027.1.17 Sat - 2027.1.18 Sun</p><h6>アーカイブ視聴期限</h6><p>2027年2月18日</p>`
	dates := extractor.ExtractEventDates(html)

	if len(dates) == 0 {
		t.Fatalf("got 0 dates, want at least 1")
	}

	if got := dates[0].Format("2006-01-02"); got != "2027-01-17" {
		t.Errorf("first date = %s, want 2027-01-17", got)
	}
	for _, d := range dates {
		if d.Format("2006-01-02") == "2027-02-18" {
			t.Errorf("archive deadline date should not be selected: %v", formatDates(dates))
		}
	}
}

func TestDateExtractor_ExtractEventDates_SectionScoring(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		expectedDates []string
	}{
		{
			name: "supernova reboot - event date 2/21 over ticket dates",
			html: `<h6>開催日時</h6><p>2026年2月21日（土）開場 16:30 / 配信開始 17:30 / 開演 18:00</p>
			       <h6>チケット</h6><p>▼ 1次先行（シリアルコード/抽選）受付期間：2025年11月14日（金）22:00 〜 2025年11月24日（月）23:59</p>
			       <h6>配信チケット</h6><p>販売期間 2025年11月14日～2026年3月21日</p>`,
			expectedDates: []string{"2026-02-21"},
		},
		{
			name: "first mission - event date 4/29 over presale dates",
			html: `<h6>開催日時</h6><p>2026年4月29日（水・祝）開場 16:30 / 配信開始 17:30 / 開演 18:00</p>
			       <h6>チケット</h6><p>▼ 1次先行（hololive FANCLUB先行 / 抽選）受付期間：2026年2月12日（木）22:00 〜 2026年2月22日（日）23:59</p>`,
			expectedDates: []string{"2026-04-29"},
		},
		{
			name:          "section boundary - header keyword in same section",
			html:          `<h6>開催日時</h6><p>2027年5月10日</p><h6>アーカイブ視聴期限</h6><p>2027年6月10日</p>`,
			expectedDates: []string{"2027-05-10"},
		},
		{
			name:          "no section headers - fallback to existing scoring",
			html:          `<p>開催日時 2027年8月15日</p><p>チケット販売 2027年6月1日</p>`,
			expectedDates: []string{"2027-08-15"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.ExtractEventDates(tt.html)

			if len(dates) != len(tt.expectedDates) {
				t.Errorf("got %d dates %v, want %d dates %v",
					len(dates), formatDates(dates), len(tt.expectedDates), tt.expectedDates)
				return
			}

			for i, expected := range tt.expectedDates {
				got := dates[i].Format("2006-01-02")
				if got != expected {
					t.Errorf("date[%d] = %s, want %s", i, got, expected)
				}
			}
		})
	}
}

func TestDateExtractor_ExtractEventDates_SectionScoring_EdgeCases(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		expectedDates []string
	}{
		{
			name:          "nested span in h6 - inner tags stripped",
			html:          `<h6><span class="title">開催日時</span></h6><p>2027年9月15日</p><h6><span>チケット</span></h6><p>2027年7月1日</p>`,
			expectedDates: []string{"2027-09-15"},
		},
		{
			name:          "duplicate header text - forward-only matching",
			html:          `<h6>開催日時</h6><p>2027年10月1日</p><h6>チケット</h6><p>2027年8月1日</p><h6>開催日時</h6><p>2027年10月2日</p>`,
			expectedDates: []string{"2027-10-01", "2027-10-02"},
		},
		{
			name:          "mixed h4 h5 h6 headers",
			html:          `<h4>開催日時</h4><p>2027年11月3日</p><h5>チケット情報</h5><p>2027年9月1日</p>`,
			expectedDates: []string{"2027-11-03"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.ExtractEventDates(tt.html)

			if len(dates) != len(tt.expectedDates) {
				t.Errorf("got %d dates %v, want %d dates %v",
					len(dates), formatDates(dates), len(tt.expectedDates), tt.expectedDates)
				return
			}

			for i, expected := range tt.expectedDates {
				got := dates[i].Format("2006-01-02")
				if got != expected {
					t.Errorf("date[%d] = %s, want %s", i, got, expected)
				}
			}
		})
	}
}

func TestDateExtractor_NegativeKeywordExpansion(t *testing.T) {
	extractor := NewDateExtractor()

	tests := []struct {
		name          string
		html          string
		expectedDates []string
	}{
		{
			name:          "視聴期限 suppresses archive deadline",
			html:          `<p>開催日時 2027年3月1日</p><p>視聴期限 2027年4月1日</p>`,
			expectedDates: []string{"2027-03-01"},
		},
		{
			name:          "購入 suppresses purchase deadline",
			html:          `<p>開催日時 2027年3月1日</p><p>購入期限 2027年2月15日</p>`,
			expectedDates: []string{"2027-03-01"},
		},
		{
			// 섹션 헤더 있을 때 配信期間 섹션 날짜 억제 검증
			name:          "配信期間 suppresses streaming period via section header",
			html:          `<h6>開催日時</h6><p>2027年3月1日</p><h6>配信期間</h6><p>2027年3月1日～2027年4月30日</p>`,
			expectedDates: []string{"2027-03-01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dates := extractor.ExtractEventDates(tt.html)

			if len(dates) != len(tt.expectedDates) {
				t.Errorf("got %d dates %v, want %d dates %v",
					len(dates), formatDates(dates), len(tt.expectedDates), tt.expectedDates)
				return
			}

			for i, expected := range tt.expectedDates {
				got := dates[i].Format("2006-01-02")
				if got != expected {
					t.Errorf("date[%d] = %s, want %s", i, got, expected)
				}
			}
		})
	}
}

// TestDateExtractor_RealHTML_SuperNova: 실제 DB에서 추출한 SuperNova HTML로 날짜 추출 검증
func TestDateExtractor_RealHTML_SuperNova(t *testing.T) {
	html, err := os.ReadFile("testdata/supernova_reboot_real.html")
	if err != nil {
		t.Skipf("testdata/supernova_reboot_real.html 없음: %v", err)
	}

	extractor := NewDateExtractor()
	dates := extractor.ExtractEventDates(string(html))

	if len(dates) == 0 {
		t.Fatal("날짜가 하나도 추출되지 않음")
	}

	// 이벤트 날짜 2026-02-21이 첫 번째(=선택된) 날짜여야 함
	first := dates[0].Format("2006-01-02")
	if first != "2026-02-21" {
		t.Errorf("첫 번째 추출 날짜 = %s, want 2026-02-21", first)
	}

	t.Logf("추출된 날짜 (%d건):", len(dates))
	for i, d := range dates {
		t.Logf("  [%d] %s", i, d.Format("2006-01-02"))
	}
}
