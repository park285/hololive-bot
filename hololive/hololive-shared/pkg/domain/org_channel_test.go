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

func TestChannel_GetDisplayName(t *testing.T) {
	t.Parallel()

	englishName := "Houshou Marine"
	emptyEnglish := ""

	tests := []struct {
		name    string
		channel *domain.Channel
		want    string
	}{
		{
			// nil 수신자는 빈 문자열 반환
			name:    "nil 수신자",
			channel: nil,
			want:    "",
		},
		{
			// EnglishName 필드 없음 → Name 반환
			name:    "영문 이름 없음",
			channel: &domain.Channel{Name: "호쇼 마린"},
			want:    "호쇼 마린",
		},
		{
			// EnglishName 포인터가 빈 문자열 → Name 반환
			name:    "영문 이름 빈 문자열",
			channel: &domain.Channel{Name: "호쇼 마린", EnglishName: &emptyEnglish},
			want:    "호쇼 마린",
		},
		{
			// 유효한 EnglishName → 영문 이름 반환
			name:    "유효한 영문 이름",
			channel: &domain.Channel{Name: "호쇼 마린", EnglishName: &englishName},
			want:    "Houshou Marine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.channel.GetDisplayName()
			if got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannel_IsHololive(t *testing.T) {
	t.Parallel()

	orgHololive := "Hololive"
	orgOther := "Other"

	tests := []struct {
		name    string
		channel *domain.Channel
		want    bool
	}{
		{
			// nil 수신자 → false
			name:    "nil 수신자",
			channel: nil,
			want:    false,
		},
		{
			// Org 포인터가 nil → false
			name:    "nil Org",
			channel: &domain.Channel{Name: "테스트"},
			want:    false,
		},
		{
			// Org = "Other" → false
			name:    "Org가 Other",
			channel: &domain.Channel{Name: "테스트", Org: &orgOther},
			want:    false,
		},
		{
			// Org = "Hololive" → true
			name:    "Org가 Hololive",
			channel: &domain.Channel{Name: "테스트", Org: &orgHololive},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.channel.IsHololive()
			if got != tt.want {
				t.Errorf("IsHololive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannel_HasPhoto(t *testing.T) {
	t.Parallel()

	emptyPhoto := ""
	validPhoto := "https://yt3.googleusercontent.com/example.jpg"

	tests := []struct {
		name    string
		channel *domain.Channel
		want    bool
	}{
		{
			// nil 수신자 → false
			name:    "nil 수신자",
			channel: nil,
			want:    false,
		},
		{
			// Photo 포인터가 nil → false
			name:    "nil Photo",
			channel: &domain.Channel{Name: "테스트"},
			want:    false,
		},
		{
			// Photo 포인터가 빈 문자열 → false
			name:    "빈 Photo",
			channel: &domain.Channel{Name: "테스트", Photo: &emptyPhoto},
			want:    false,
		},
		{
			// 유효한 Photo URL → true
			name:    "유효한 Photo",
			channel: &domain.Channel{Name: "테스트", Photo: &validPhoto},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.channel.HasPhoto()
			if got != tt.want {
				t.Errorf("HasPhoto() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannel_GetPhotoURL(t *testing.T) {
	t.Parallel()

	validPhoto := "https://yt3.googleusercontent.com/example.jpg"

	tests := []struct {
		name    string
		channel *domain.Channel
		want    string
	}{
		{
			// nil 수신자 → 빈 문자열
			name:    "nil 수신자",
			channel: nil,
			want:    "",
		},
		{
			// Photo 없음 → 빈 문자열
			name:    "Photo 없음",
			channel: &domain.Channel{Name: "테스트"},
			want:    "",
		},
		{
			// 유효한 Photo → URL 반환
			name:    "유효한 Photo",
			channel: &domain.Channel{Name: "테스트", Photo: &validPhoto},
			want:    "https://yt3.googleusercontent.com/example.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.channel.GetPhotoURL()
			if got != tt.want {
				t.Errorf("GetPhotoURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
