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

package domain

func templateSampleMajorEventData() map[TemplateKey]any {
	data := map[TemplateKey]any{}
	addTemplateMajorEventSummarySamples(data)
	addTemplateMajorEventSubscriptionSamples(data)
	return data
}

func addTemplateMajorEventSummarySamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdMajorEventWeeklySummary] = templateMajorEventSummarySample()
	data[TemplateKeyCmdMajorEventMonthlySummary] = templateMajorEventSummarySample()
}

func templateMajorEventSummarySample() map[string]any {
	return map[string]any{
		"Emoji":      map[string]string{"Schedule": "📅", "Member": "👥", "Link": "🔗"},
		"Count":      2,
		"LLMSummary": "",
		"Events":     templateMajorEventItems(),
	}
}

func templateMajorEventItems() []map[string]any {
	return []map[string]any{
		{
			"Title":    "hololive SUPER EXPO 2026",
			"DateStr":  "2026년 3월 7일(토) ~ 2026년 3월 8일(일)",
			"Members":  "사쿠라 미코, 호시마치 스이세이",
			"Link":     "https://hololive.hololivepro.com/events/",
			"HasDates": true,
		},
		{
			"Title":    "hololive 7th fes.",
			"DateStr":  "TBA",
			"Members":  "hololive members",
			"Link":     "https://hololive.hololivepro.com/",
			"HasDates": false,
		},
	}
}

func addTemplateMajorEventSubscriptionSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdMajorEventSubscribed] = templateMajorEventAlarmSuccessSample()
	data[TemplateKeyCmdMajorEventUnsubscribed] = templateMajorEventAlarmSuccessSample()
	data[TemplateKeyCmdMajorEventAlreadySub] = templateMajorEventAlarmInfoSample()
	data[TemplateKeyCmdMajorEventNotSub] = templateMajorEventAlarmInfoSample()
	data[TemplateKeyCmdMajorEventStatus] = map[string]any{
		"Emoji":        map[string]string{"Alarm": "🔔", "Info": "ℹ️"},
		"IsSubscribed": true,
		"Prefix":       "!",
	}
	data[TemplateKeyCmdMajorEventUsage] = map[string]any{
		"Emoji":  map[string]string{"Alarm": "🔔", "Hint": "💡"},
		"Prefix": "!",
	}
}

func templateMajorEventAlarmSuccessSample() map[string]any {
	return map[string]any{
		"Emoji":  map[string]string{"Alarm": "🔔", "Success": "✅"},
		"Prefix": "!",
	}
}

func templateMajorEventAlarmInfoSample() map[string]any {
	return map[string]any{
		"Emoji":  map[string]string{"Alarm": "🔔", "Info": "ℹ️"},
		"Prefix": "!",
	}
}
