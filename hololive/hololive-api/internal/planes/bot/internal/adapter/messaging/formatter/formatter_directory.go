package formatter

import (
	"context"
	"strings"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
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

	displayCount := 0

	for _, group := range viewGroups {
		displayCount += len(group.Members)
	}

	if total <= 0 {
		total += displayCount
	}

	data := memberDirectoryTemplateData{
		Total:  total,
		Groups: viewGroups,
	}

	return f.renderResponse(ctx, domain.TemplateKeyCmdMemberDirectory, data, templateview.ShouldFoldItems(displayCount))
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

	name := strings.TrimSpace(group.GroupName)
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
	primary := strings.TrimSpace(member.PrimaryName)
	secondary := strings.TrimSpace(member.SecondaryName)

	if primary == "" && secondary == "" {
		return memberDirectoryEntryView{}, false
	}

	return memberDirectoryEntryView{
		Primary:   primary,
		Secondary: secondary,
		ShowBoth:  primary != "" && secondary != "" && !strings.EqualFold(primary, secondary),
	}, true
}
