package formatter

import (
	"context"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (f *ResponseFormatter) FormatAlarmList(ctx context.Context, alarms []AlarmListEntry) string {
	processed := make([]alarmListEntryView, len(alarms))
	for idx, alarm := range alarms {
		processed[idx] = alarmListEntryView{MemberName: alarm.MemberName}

		if !isAllAlarmTypes(alarm.AlarmTypes) {
			processed[idx].TypesLabel = f.formatAlarmTypesLabel(ctx, alarm.AlarmTypes)
		}
	}

	data := alarmListTemplateData{
		Count:  len(processed),
		Prefix: f.prefix,
		Alarms: processed,
	}

	return f.renderResponse(ctx, domain.TemplateKeyCmdAlarmList, data, templateview.ShouldFoldItems(data.Count))
}

func (f *ResponseFormatter) FormatAlarmCleared(ctx context.Context, count int) string {
	data := alarmClearedTemplateData{Count: count}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmCleared, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
