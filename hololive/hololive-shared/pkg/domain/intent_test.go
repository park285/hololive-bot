package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// TestNormalizeMemberIntent: 문자열 의도를 MemberIntentType 열거형으로 변환하는 로직을 검증합니다.
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

// TestMemberIntent_IsMemberInfoIntent: 멤버 정보 조회 의도 판별 로직과 신뢰도 임계값(0.35)을 검증합니다.
func TestMemberIntent_IsMemberInfoIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mic    *domain.MemberIntent
		want   bool
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
