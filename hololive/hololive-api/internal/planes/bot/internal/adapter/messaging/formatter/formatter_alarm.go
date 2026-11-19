package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type AlarmListEntry struct {
	MemberName string
	AlarmTypes domain.AlarmTypes
}

type alarmAddedTemplateData struct {
	MemberName string
	Added      bool
	Prefix     string
}

type alarmRemovedTemplateData struct {
	MemberName string
	Removed    bool
}

type alarmListTemplateData struct {
	Count  int
	Prefix string
	Alarms []alarmListEntryView
}

type alarmListEntryView struct {
	MemberName string
	TypesLabel string
}

type alarmClearedTemplateData struct {
	Count int
}

func (f *ResponseFormatter) FormatAlarmAdded(ctx context.Context, memberName string, added bool) string {
	data := alarmAddedTemplateData{
		MemberName: memberName,
		Added:      added,
		Prefix:     f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmAdded, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatAlarmRemoved(ctx context.Context, memberName string, removed bool) string {
	data := alarmRemovedTemplateData{
		MemberName: memberName,
		Removed:    removed,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmRemoved, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
