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

package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestOutboxKind_ToAlarmType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind domain.OutboxKind
		want domain.AlarmType
	}{
		{
			// NEW_VIDEO → AlarmTypeLive (default)
			name: "NEW_VIDEO → AlarmTypeLive",
			kind: domain.OutboxKindNewVideo,
			want: domain.AlarmTypeLive,
		},
		{
			// NEW_SHORT → AlarmTypeShorts
			name: "NEW_SHORT → AlarmTypeShorts",
			kind: domain.OutboxKindNewShort,
			want: domain.AlarmTypeShorts,
		},
		{
			// COMMUNITY_POST → AlarmTypeCommunity
			name: "COMMUNITY_POST → AlarmTypeCommunity",
			kind: domain.OutboxKindCommunityPost,
			want: domain.AlarmTypeCommunity,
		},
		{
			// MILESTONE → AlarmTypeLive (default)
			name: "MILESTONE → AlarmTypeLive",
			kind: domain.OutboxKindMilestone,
			want: domain.AlarmTypeLive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.kind.ToAlarmType()
			if got != tt.want {
				t.Errorf("ToAlarmType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOutboxKind_ToTemplateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind domain.OutboxKind
		want domain.TemplateKey
	}{
		{
			// NEW_VIDEO → TemplateKeyOutboxVideo
			name: "NEW_VIDEO → TemplateKeyOutboxVideo",
			kind: domain.OutboxKindNewVideo,
			want: domain.TemplateKeyOutboxVideo,
		},
		{
			// NEW_SHORT → TemplateKeyOutboxShorts
			name: "NEW_SHORT → TemplateKeyOutboxShorts",
			kind: domain.OutboxKindNewShort,
			want: domain.TemplateKeyOutboxShorts,
		},
		{
			// COMMUNITY_POST → TemplateKeyOutboxCommunity
			name: "COMMUNITY_POST → TemplateKeyOutboxCommunity",
			kind: domain.OutboxKindCommunityPost,
			want: domain.TemplateKeyOutboxCommunity,
		},
		{
			// MILESTONE → TemplateKeyOutboxMilestone
			name: "MILESTONE → TemplateKeyOutboxMilestone",
			kind: domain.OutboxKindMilestone,
			want: domain.TemplateKeyOutboxMilestone,
		},
		{
			// 알 수 없는 종류 → TemplateKeyOutboxVideo (default)
			name: "알 수 없는 종류 → TemplateKeyOutboxVideo",
			kind: domain.OutboxKind("UNKNOWN"),
			want: domain.TemplateKeyOutboxVideo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.kind.ToTemplateKey()
			if got != tt.want {
				t.Errorf("ToTemplateKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestYouTubeNotificationOutbox_DedupeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		item    domain.YouTubeNotificationOutbox
		want    string
		wantErr bool
	}{
		{
			name: "shorts outbox uses kind and content id",
			item: domain.YouTubeNotificationOutbox{
				Kind:      domain.OutboxKindNewShort,
				ContentID: "short-123",
			},
			want: "youtube-notification:NEW_SHORT:short-123",
		},
		{
			name: "community outbox trims content id",
			item: domain.YouTubeNotificationOutbox{
				Kind:      domain.OutboxKindCommunityPost,
				ContentID: "  post-123  ",
			},
			want: "youtube-notification:COMMUNITY_POST:post-123",
		},
		{
			name: "empty kind is rejected",
			item: domain.YouTubeNotificationOutbox{
				ContentID: "post-123",
			},
			wantErr: true,
		},
		{
			name: "empty content id is rejected",
			item: domain.YouTubeNotificationOutbox{
				Kind:      domain.OutboxKindCommunityPost,
				ContentID: "   ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.item.DedupeKey()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DedupeKey() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DedupeKey() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DedupeKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestThumbnailsJSON_Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		thumbnails domain.ThumbnailsJSON
		wantNil    bool
	}{
		{
			// nil 슬라이스 → (nil, nil) 반환
			name:       "nil 슬라이스",
			thumbnails: nil,
			wantNil:    true,
		},
		{
			// 유효한 항목 → JSON 문자열 반환
			name: "유효한 썸네일",
			thumbnails: domain.ThumbnailsJSON{
				{URL: "https://i.ytimg.com/vi/abc/hqdefault.jpg", Width: 480, Height: 360},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, err := tt.thumbnails.Value()
			if err != nil {
				t.Fatalf("Value() 오류: %v", err)
			}
			if tt.wantNil {
				if val != nil {
					t.Errorf("Value() = %v, want nil", val)
				}
				return
			}
			// 유효한 경우 string 타입이어야 한다 (pgx jsonb 컬럼 요구사항)
			s, ok := val.(string)
			if !ok {
				t.Fatalf("Value() 타입 = %T, want string", val)
			}
			if len(s) == 0 {
				t.Error("Value() 반환 JSON 문자열이 비어있음")
			}
		})
	}
}

func TestThumbnailsJSON_Scan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		wantNil bool
		wantErr bool
	}{
		{
			// nil 입력 → nil ThumbnailsJSON, 오류 없음
			name:    "nil 입력",
			input:   nil,
			wantNil: true,
			wantErr: false,
		},
		{
			// 유효한 JSON 바이트 → 파싱 성공
			name:    "유효한 JSON 바이트",
			input:   []byte(`[{"url":"https://i.ytimg.com/vi/abc/hqdefault.jpg","width":480,"height":360}]`),
			wantNil: false,
			wantErr: false,
		},
		{
			// 잘못된 타입 (string) → 오류 반환
			name:    "잘못된 타입",
			input:   "문자열은 지원 안 함",
			wantNil: false,
			wantErr: true,
		},
		{
			// 유효하지 않은 JSON → 오류 반환
			name:    "잘못된 JSON",
			input:   []byte(`{not valid json`),
			wantNil: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var result domain.ThumbnailsJSON
			err := result.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan() 오류 = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantNil && result != nil {
				t.Errorf("Scan() result = %v, want nil", result)
			}
			if !tt.wantNil && result == nil {
				t.Error("Scan() result가 nil, 파싱된 값이 있어야 함")
			}
		})
	}
}
