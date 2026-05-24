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
	"strings"

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

type MemberDirectoryGroup struct {
	GroupName string
	Members   []MemberDirectoryEntry
}

type MemberDirectoryEntry struct {
	PrimaryName   string
	SecondaryName string
}

type memberDirectoryTemplateData struct {
	Emoji  msging.UIEmoji
	Total  int
	Groups []memberDirectoryGroupView
}

type memberDirectoryGroupView struct {
	GroupName string
	Members   []memberDirectoryEntryView
}

type memberDirectoryEntryView struct {
	Primary   string
	Secondary string
	ShowBoth  bool
}

func (f *ResponseFormatter) MemberDirectory(ctx context.Context, groups []MemberDirectoryGroup, total int) string {
	viewGroups := prepareMemberDirectoryGroups(groups)

	if total <= 0 {
		for _, group := range viewGroups {
			total += len(group.Members)
		}
	}

	data := memberDirectoryTemplateData{
		Emoji:  msging.DefaultEmoji,
		Total:  total,
		Groups: viewGroups,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberDirectory, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMemberListFailed)
	}

	if len(viewGroups) == 0 {
		return rendered
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func prepareMemberDirectoryGroups(groups []MemberDirectoryGroup) []memberDirectoryGroupView {
	if len(groups) == 0 {
		return nil
	}

	views := make([]memberDirectoryGroupView, 0, len(groups))
	for _, group := range groups {
		view, ok := prepareMemberDirectoryGroup(group)
		if !ok {
			continue
		}
		views = append(views, view)
	}

	return views
}

func prepareMemberDirectoryGroup(group MemberDirectoryGroup) (memberDirectoryGroupView, bool) {
	members := prepareMemberDirectoryEntries(group.Members)
	if len(members) == 0 {
		return memberDirectoryGroupView{}, false
	}

	name := stringutil.TrimSpace(group.GroupName)
	if name == "" {
		name = "기타"
	}

	return memberDirectoryGroupView{GroupName: name, Members: members}, true
}

func prepareMemberDirectoryEntries(members []MemberDirectoryEntry) []memberDirectoryEntryView {
	views := make([]memberDirectoryEntryView, 0, len(members))
	for _, member := range members {
		entry, ok := prepareMemberDirectoryEntry(member)
		if !ok {
			continue
		}
		views = append(views, entry)
	}
	return views
}

func prepareMemberDirectoryEntry(member MemberDirectoryEntry) (memberDirectoryEntryView, bool) {
	primary := stringutil.TrimSpace(member.PrimaryName)
	secondary := stringutil.TrimSpace(member.SecondaryName)
	if primary == "" && secondary == "" {
		return memberDirectoryEntryView{}, false
	}

	return memberDirectoryEntryView{
		Primary:   primary,
		Secondary: secondary,
		ShowBoth:  primary != "" && secondary != "" && !strings.EqualFold(primary, secondary),
	}, true
}
