package adapter

import (
	"context"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// MemberDirectoryGroup: 멤버 목록 표시를 위한 그룹 (예: 'JP 3기생', 'EN Promise')
type MemberDirectoryGroup struct {
	GroupName string
	Members   []MemberDirectoryEntry
}

// MemberDirectoryEntry: 멤버 목록의 개별 항목 (주 이름 및 보조 이름 포함)
type MemberDirectoryEntry struct {
	PrimaryName   string
	SecondaryName string
}

type memberDirectoryTemplateData struct {
	Emoji  UIEmoji
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
		Emoji:  DefaultEmoji,
		Total:  total,
		Groups: viewGroups,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberDirectory, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMemberListFailed)
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
		name := stringutil.TrimSpace(group.GroupName)
		if name == "" {
			name = "기타"
		}

		members := make([]memberDirectoryEntryView, 0, len(group.Members))
		for _, member := range group.Members {
			primary := stringutil.TrimSpace(member.PrimaryName)
			secondary := stringutil.TrimSpace(member.SecondaryName)
			if primary == "" && secondary == "" {
				continue
			}

			entry := memberDirectoryEntryView{
				Primary:   primary,
				Secondary: secondary,
				ShowBoth:  primary != "" && secondary != "" && !strings.EqualFold(primary, secondary),
			}
			members = append(members, entry)
		}

		if len(members) == 0 {
			continue
		}

		views = append(views, memberDirectoryGroupView{
			GroupName: name,
			Members:   members,
		})
	}

	return views
}
