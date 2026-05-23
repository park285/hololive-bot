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

package model

import "github.com/park285/hololive-bot/shared-go/pkg/stringutil"

type MemberIntentType string

// MemberIntentType 상수 목록.
const (
	// MemberIntentUnknown: 의도를 파악할 수 없음
	MemberIntentUnknown MemberIntentType = "unknown"
	// MemberIntentMemberInfo: 멤버 상세 정보 조회 의도
	MemberIntentMemberInfo MemberIntentType = "member_info"
	// MemberIntentOther: 그 외 기타 의도
	MemberIntentOther MemberIntentType = "other"
)

type MemberIntent struct {
	Intent     MemberIntentType `json:"intent"`
	Confidence float64          `json:"confidence"`
	Reasoning  string           `json:"reasoning"`
}

func NormalizeMemberIntent(raw string) MemberIntentType {
	switch stringutil.Normalize(raw) {
	case string(MemberIntentMemberInfo):
		return MemberIntentMemberInfo
	case string(MemberIntentOther):
		return MemberIntentOther
	default:
		return MemberIntentUnknown
	}
}

func (mic *MemberIntent) IsMemberInfoIntent() bool {
	if mic == nil {
		return false
	}
	intent := NormalizeMemberIntent(string(mic.Intent))
	if intent != MemberIntentMemberInfo {
		return false
	}
	return mic.Confidence >= 0.35
}
