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

func TestMember_GetAllAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		member *domain.Member
		want   []string
	}{
		{
			// Aliases가 nil → 빈 슬라이스 반환
			name:   "nil Aliases",
			member: &domain.Member{Name: "페코라", Aliases: nil},
			want:   []string{},
		},
		{
			// Ko, Ja 별명 모두 있을 때 → 합산 반환 (Ko 먼저)
			name: "Ko, Ja 별명 모두 있음",
			member: &domain.Member{
				Name: "페코라",
				Aliases: &domain.Aliases{
					Ko: []string{"페코", "페코라"},
					Ja: []string{"ぺこら", "ぺこ"},
				},
			},
			want: []string{"페코", "페코라", "ぺこら", "ぺこ"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.member.GetAllAliases()
			if len(got) != len(tt.want) {
				t.Fatalf("GetAllAliases() 길이 = %d, want %d", len(got), len(tt.want))
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("GetAllAliases()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestMember_HasAlias(t *testing.T) {
	t.Parallel()

	member := &domain.Member{
		Name: "우사다 페코라",
		Aliases: &domain.Aliases{
			Ko: []string{"페코", "페코라"},
			Ja: []string{"ぺこら", "ぺこ"},
		},
	}

	tests := []struct {
		name  string
		alias string
		want  bool
	}{
		{
			// Ko 별명에서 발견 → true
			name:  "Ko 별명 일치",
			alias: "페코",
			want:  true,
		},
		{
			// Ja 별명에서 발견 → true
			name:  "Ja 별명 일치",
			alias: "ぺこら",
			want:  true,
		},
		{
			// 존재하지 않는 별명 → false
			name:  "별명 없음",
			alias: "마린",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := member.HasAlias(tt.alias)
			if got != tt.want {
				t.Errorf("HasAlias(%q) = %v, want %v", tt.alias, got, tt.want)
			}
		})
	}
}

func TestMember_GetOrg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		member *domain.Member
		want   string
	}{
		{
			// Org 빈 문자열 → 기본값 "Hololive" 반환
			name:   "빈 Org",
			member: &domain.Member{Name: "테스트", Org: ""},
			want:   "Hololive",
		},
		{
			// Org에 값이 있으면 해당 값 반환
			name:   "비어있지 않은 Org",
			member: &domain.Member{Name: "테스트", Org: "Nijisanji"},
			want:   "Nijisanji",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.member.GetOrg()
			if got != tt.want {
				t.Errorf("GetOrg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMember_GetDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		member *domain.Member
		want   string
	}{
		{
			// "이름 (그룹)" 형식 반환
			name:   "Org가 있을 때",
			member: &domain.Member{Name: "우사다 페코라", Org: "Hololive"},
			want:   "우사다 페코라 (Hololive)",
		},
		{
			// Org 빈 문자열이면 기본값 "Hololive" 사용
			name:   "Org 빈 문자열이면 기본값 사용",
			member: &domain.Member{Name: "테스트 멤버", Org: ""},
			want:   "테스트 멤버 (Hololive)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.member.GetDisplayName()
			if got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMember_GetChzzkLiveURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		member *domain.Member
		want   string
	}{
		{
			name:   "ChzzkChannelID 있음",
			member: &domain.Member{ChzzkChannelID: "abc123"},
			want:   "https://chzzk.naver.com/live/abc123",
		},
		{
			// 빈 ID는 기존 fmt.Sprintf와 동일하게 trailing slash까지만 생성 (guard 없음 = drop-in)
			name:   "빈 ChzzkChannelID",
			member: &domain.Member{ChzzkChannelID: ""},
			want:   "https://chzzk.naver.com/live/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.member.GetChzzkLiveURL()
			if got != tt.want {
				t.Errorf("GetChzzkLiveURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
