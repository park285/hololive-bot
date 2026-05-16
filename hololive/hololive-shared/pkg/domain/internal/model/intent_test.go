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

package model_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNormalizeMemberIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.MemberIntentType
	}{
		{"member_info 정확히 일치", "member_info", domain.MemberIntentMemberInfo},
		{"other 정확히 일치", "other", domain.MemberIntentOther},
		{"unknown 정확히 일치", "unknown", domain.MemberIntentUnknown},
		{"빈 문자열은 unknown", "", domain.MemberIntentUnknown},
		{"대문자는 소문자로 정규화", "MEMBER_INFO", domain.MemberIntentMemberInfo},
		{"알 수 없는 값은 unknown", "random", domain.MemberIntentUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.NormalizeMemberIntent(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeMemberIntent(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMemberIntent_IsMemberInfoIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mic  *domain.MemberIntent
		want bool
	}{
		{
			name: "nil 포인터는 false",
			mic:  nil,
			want: false,
		},
		{
			name: "other 의도는 false",
			mic:  &domain.MemberIntent{Intent: "other", Confidence: 0.9},
			want: false,
		},
		{
			name: "신뢰도 0.34는 임계값 미달로 false",
			mic:  &domain.MemberIntent{Intent: "member_info", Confidence: 0.34},
			want: false,
		},
		{
			name: "신뢰도 0.35는 임계값 경계로 true",
			mic:  &domain.MemberIntent{Intent: "member_info", Confidence: 0.35},
			want: true,
		},
		{
			name: "신뢰도 0.9는 임계값 초과로 true",
			mic:  &domain.MemberIntent{Intent: "member_info", Confidence: 0.9},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.mic.IsMemberInfoIntent()
			if got != tt.want {
				t.Errorf("MemberIntent.IsMemberInfoIntent() = %v, want %v", got, tt.want)
			}
		})
	}
}
