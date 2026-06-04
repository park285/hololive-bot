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

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (f *ResponseFormatter) FormatAlarmList(ctx context.Context, alarms []AlarmListEntry) string {
	processed := make([]alarmListEntryView, len(alarms))
	for idx, alarm := range alarms {
		processed[idx] = alarmListEntryView{
			MemberName: alarm.MemberName,
			TypesLabel: formatAlarmTypesLabel(alarm.AlarmTypes),
			NextStream: buildNextStreamInfoView(summarizeNextStreamInfo(alarm.NextStream)),
		}
	}

	data := alarmListTemplateData{
		Emoji:  msging.DefaultEmoji,
		Count:  len(processed),
		Prefix: f.prefix,
		Alarms: processed,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmList, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayAlarmListFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatAlarmCleared(ctx context.Context, count int) string {
	data := alarmClearedTemplateData{Emoji: msging.DefaultEmoji, Count: count}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmCleared, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayAlarmClearFailed)
	}

	return rendered
}

func (f *ResponseFormatter) InvalidAlarmUsage() string {
	return msging.ErrInvalidAlarmUsage
}
