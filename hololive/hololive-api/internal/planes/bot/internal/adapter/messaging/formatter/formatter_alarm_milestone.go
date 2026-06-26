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

package formatter

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type milestoneAchievedTemplateData struct {
	MemberName string
	Milestone  string
}

type milestoneApproachingTemplateData struct {
	MemberName string
	Milestone  string
	Remaining  string
}

func (f *ResponseFormatter) FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error) {
	data := milestoneAchievedTemplateData{
		MemberName: memberName,
		Milestone:  milestone,
	}

	return f.render(ctx, domain.TemplateKeyCmdMilestoneAchieved, data)
}

func (f *ResponseFormatter) FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error) {
	data := milestoneApproachingTemplateData{
		MemberName: memberName,
		Milestone:  milestone,
		Remaining:  remaining,
	}

	return f.render(ctx, domain.TemplateKeyCmdMilestoneApproach, data)
}

func formatAlarmTypesLabel(types domain.AlarmTypes) string {
	if len(types) == 0 || len(types) == len(domain.AllAlarmTypes) {
		return "전체"
	}

	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.DisplayName()
	}

	return strings.Join(names, "+")
}

func (f *ResponseFormatter) FormatAmbiguousMembers(candidates []*domain.Member) string {
	var sb strings.Builder
	sb.WriteString("동일한 이름의 멤버가 여러 명 있습니다:\n\n")

	for i, m := range candidates {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, m.GetDisplayName())
	}

	sb.WriteString("\n정확한 멤버를 지정하려면 다음과 같이 입력해주세요:\n")

	if len(candidates) > 0 {
		fmt.Fprintf(&sb, "%s알람 추가 %s", f.prefix, candidates[0].GetDisplayName())
	}

	return sb.String()
}
