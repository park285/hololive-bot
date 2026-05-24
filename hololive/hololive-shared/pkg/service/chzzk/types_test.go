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

package chzzk

import (
	"os"
	"testing"
	"time"

	json "github.com/park285/shared-go/pkg/json"
)

func TestLiveStatusResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		wantCode int
		wantNil  bool
	}{
		{
			name:     "OPEN 상태",
			fixture:  "testdata/live_status_open.json",
			wantCode: 200,
			wantNil:  false,
		},
		{
			name:     "CLOSE 상태",
			fixture:  "testdata/live_status_close.json",
			wantCode: 200,
			wantNil:  false,
		},
		{
			name:     "RESERVE 상태",
			fixture:  "testdata/live_status_reserve.json",
			wantCode: 200,
			wantNil:  false,
		},
		{
			name:     "404 에러 (nil content)",
			fixture:  "testdata/error_404.json",
			wantCode: 404,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("Failed to read fixture: %v", err)
			}

			var resp LiveStatusResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if resp.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", resp.Code, tt.wantCode)
			}

			if tt.wantNil && resp.Content != nil {
				t.Error("Content should be nil for 404")
			}

			if !tt.wantNil && resp.Content == nil {
				t.Error("Content should not be nil")
			}
		})
	}
}

func TestLiveStatusContent_Fields(t *testing.T) {
	data, err := os.ReadFile("testdata/live_status_open.json")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	var resp LiveStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if resp.Content == nil {
		t.Fatal("Content is nil")
	}

	content := resp.Content
	if content.Status != "OPEN" {
		t.Errorf("Status = %s, want OPEN", content.Status)
	}

	if content.LiveTitle == "" {
		t.Error("LiveTitle should not be empty")
	}

	if content.ConcurrentUserCount <= 0 {
		t.Error("ConcurrentUserCount should be positive for OPEN status")
	}

	if content.ChatChannelId == "" {
		t.Error("ChatChannelId should not be empty")
	}
}

func TestScheduledLivesResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantCode  int
		wantCount int
	}{
		{
			name:      "2개의 예정 방송",
			fixture:   "testdata/scheduled_lives.json",
			wantCode:  200,
			wantCount: 2,
		},
		{
			name:      "빈 배열",
			fixture:   "testdata/scheduled_lives_empty.json",
			wantCode:  200,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("Failed to read fixture: %v", err)
			}

			var resp ScheduledLivesResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if resp.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", resp.Code, tt.wantCode)
			}

			if resp.Content == nil {
				t.Fatal("Content should not be nil")
			}

			if len(resp.Content.ScheduledLives) != tt.wantCount {
				t.Errorf("ScheduledLives count = %d, want %d", len(resp.Content.ScheduledLives), tt.wantCount)
			}
		})
	}
}

func TestScheduledLive_Fields(t *testing.T) {
	data, err := os.ReadFile("testdata/scheduled_lives.json")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	var resp ScheduledLivesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(resp.Content.ScheduledLives) == 0 {
		t.Fatal("ScheduledLives should not be empty")
	}

	first := resp.Content.ScheduledLives[0]
	if first.LiveId <= 0 {
		t.Error("LiveId should be positive")
	}

	if first.LiveTitle == "" {
		t.Error("LiveTitle should not be empty")
	}

	if first.ScheduledStartAt == "" {
		t.Error("ScheduledStartAt should not be empty")
	}
}

func TestParseScheduledStartAt(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantHour  int
		wantMin   int
	}{
		{
			name:      "정상 파싱 - 2024-05-20 19:00:00",
			input:     "2024-05-20 19:00:00",
			wantErr:   false,
			wantYear:  2024,
			wantMonth: time.May,
			wantDay:   20,
			wantHour:  19,
			wantMin:   0,
		},
		{
			name:      "정상 파싱 - 2024-05-21 20:30:00",
			input:     "2024-05-21 20:30:00",
			wantErr:   false,
			wantYear:  2024,
			wantMonth: time.May,
			wantDay:   21,
			wantHour:  20,
			wantMin:   30,
		},
		{
			name:    "잘못된 형식 - ISO8601",
			input:   "2024-05-20T19:00:00Z",
			wantErr: true,
		},
		{
			name:    "잘못된 형식 - 빈 문자열",
			input:   "",
			wantErr: true,
		},
		{
			name:    "잘못된 형식 - 날짜만",
			input:   "2024-05-20",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseScheduledStartAt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseScheduledStartAt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// KST 타임존 검증
			if got.Location().String() != "Asia/Seoul" {
				t.Errorf("Location = %s, want Asia/Seoul", got.Location())
			}

			if got.Year() != tt.wantYear {
				t.Errorf("Year = %d, want %d", got.Year(), tt.wantYear)
			}

			if got.Month() != tt.wantMonth {
				t.Errorf("Month = %v, want %v", got.Month(), tt.wantMonth)
			}

			if got.Day() != tt.wantDay {
				t.Errorf("Day = %d, want %d", got.Day(), tt.wantDay)
			}

			if got.Hour() != tt.wantHour {
				t.Errorf("Hour = %d, want %d", got.Hour(), tt.wantHour)
			}

			if got.Minute() != tt.wantMin {
				t.Errorf("Minute = %d, want %d", got.Minute(), tt.wantMin)
			}
		})
	}
}

func TestParseScheduledStartAt_TimeZone(t *testing.T) {
	parsed, err := ParseScheduledStartAt("2024-05-20 19:00:00")
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// KST는 UTC+9
	utc := parsed.UTC()
	if utc.Hour() != 10 {
		t.Errorf("UTC Hour = %d, want 10 (KST 19:00 = UTC 10:00)", utc.Hour())
	}
}
