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

package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageBuilderCoreMethods(t *testing.T) {
	t.Parallel()

	builder := NewMessageBuilder()
	require.NotNil(t, builder)

	assert.Equal(t, "🔔 알람 (3개)", builder.CountedHeader("🔔", "알람", 3))
	assert.Equal(t, "📺 예정 방송 (12시간 이내, 4개)", builder.TimeRangeHeader("📺", "예정 방송", 12, 4))
	assert.Equal(t, "📅 사쿠라 미코 일정 (7일 이내, 5개)", builder.DayRangeHeader("📅", "사쿠라 미코", 7, 5))
	assert.Equal(t, "📅 일정 (7일 이내, 5개)", builder.DayRangeHeader("📅", "", 7, 5))
	assert.Equal(t, "ℹ️ 데이터 없음", builder.EmptyMessage("ℹ️", "데이터 없음"))
	assert.Equal(t, "❌ 처리 실패", builder.ErrorMessage("처리 실패"))
	assert.Equal(t, "✅ 처리 완료", builder.SuccessMessage("처리 완료"))

	usage := builder.UsageHint("!", "일정", "일정 페코라")
	assert.Contains(t, usage, "💡 사용법:")
	assert.Contains(t, usage, "!일정 [멤버명]")
	assert.Contains(t, usage, "예) !일정 페코라")
}

func TestMessageBuilderMemberHeaderAndJoinNames(t *testing.T) {
	t.Parallel()

	builder := NewMessageBuilder()

	assert.Equal(t, "📘 멤버 정보", builder.MemberHeader(nil))
	assert.Equal(t, "📘 사쿠라 미코", builder.MemberHeader([]string{"사쿠라 미코"}))
	assert.Equal(t, "📘 사쿠라 미코 (호시마치 스이세이 / 시라카미 후부키)", builder.MemberHeader([]string{"사쿠라 미코", "호시마치 스이세이", "시라카미 후부키"}))

	assert.Empty(t, joinNames(nil))
	assert.Equal(t, "A", joinNames([]string{"A"}))
	assert.Equal(t, "A / B / C", joinNames([]string{"A", "B", "C"}))
}

func TestGlobalMessageBuilderWrappers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, defaultMessageBuilder.CountedHeader("🔔", "테스트", 2), CountedHeader("🔔", "테스트", 2))
	assert.Equal(t, defaultMessageBuilder.TimeRangeHeader("📺", "예정", 3, 1), TimeRangeHeader("📺", "예정", 3, 1))
	assert.Equal(t, defaultMessageBuilder.DayRangeHeader("📅", "후부키", 2, 1), DayRangeHeader("📅", "후부키", 2, 1))
	assert.Equal(t, defaultMessageBuilder.EmptyMessage("ℹ️", "없음"), EmptyMessage("ℹ️", "없음"))
	assert.Equal(t, defaultMessageBuilder.UsageHint("!", "도움", "도움"), UsageHint("!", "도움", "도움"))
	assert.Equal(t, defaultMessageBuilder.ErrorMessage("에러"), ErrorMessage("에러"))
	assert.Equal(t, defaultMessageBuilder.SuccessMessage("성공"), SuccessMessage("성공"))
	assert.Equal(t, defaultMessageBuilder.MemberHeader([]string{"A", "B"}), MemberHeader([]string{"A", "B"}))
}
