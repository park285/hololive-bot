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

package sampledata

import "github.com/kapu/hololive-shared/pkg/domain"

func templateSampleMemberNewsData() map[domain.TemplateKey]any {
	data := map[domain.TemplateKey]any{
		domain.TemplateKeyCmdMemberNewsDigest: templateMemberNewsDigestSample(),
	}
	addTemplateMemberNewsSubscriptionSamples(data)
	return data
}

func templateMemberNewsDigestSample() map[string]any {
	return map[string]any{
		fieldEmoji:    map[string]string{"Brand": "🌸", "Link": "🔗"},
		"Headline":    "🗞️ 이번주 구독 멤버 뉴스",
		"TopItems":    templateMemberNewsDigestItems(),
		"MoreSummary": "외 3건",
		"TotalCount":  5,
	}
}

func templateMemberNewsDigestItems() []map[string]any {
	return []map[string]any{
		{
			"Member":    sampleMemberMiko,
			"Category":  "birthday_live",
			fieldTitle:  "さくらみこ生誕ライブ2026",
			"DateText":  "2026-02-20",
			"Summary":   "생일 기념 라이브 진행 예정",
			"SourceURL": "https://hololive.hololivepro.com/news/",
		},
		{
			"Member":    "시라카미 후부키",
			"Category":  "event",
			fieldTitle:  "hololive SUPER EXPO 2026",
			"DateText":  "2026-03-07",
			"Summary":   "엑스포 참여 소식",
			"SourceURL": "https://hololive.hololivepro.com/events/",
		},
	}
}

func addTemplateMemberNewsSubscriptionSamples(data map[domain.TemplateKey]any) {
	data[domain.TemplateKeyCmdMemberNewsNoMembers] = map[string]any{
		fieldEmoji:  map[string]string{"Brand": "🌸"},
		fieldPrefix: "!",
	}
	data[domain.TemplateKeyCmdMemberNewsSubscribed] = templateMemberNewsAlarmSuccessSample()
	data[domain.TemplateKeyCmdMemberNewsUnsubscribed] = templateMemberNewsAlarmSuccessSample()
	data[domain.TemplateKeyCmdMemberNewsAlreadySub] = templateMemberNewsAlarmInfoSample("🔔")
	data[domain.TemplateKeyCmdMemberNewsNotSub] = templateMemberNewsAlarmInfoSample("🔕")
	data[domain.TemplateKeyCmdMemberNewsStatus] = map[string]any{
		fieldEmoji:     map[string]string{fieldAlarm: "🔔"},
		fieldPrefix:    "!",
		"IsSubscribed": true,
	}
}

func templateMemberNewsAlarmSuccessSample() map[string]any {
	return map[string]any{
		fieldEmoji:  map[string]string{fieldAlarm: "🔔", "Success": "✅"},
		fieldPrefix: "!",
	}
}

func templateMemberNewsAlarmInfoSample(alarmEmoji string) map[string]any {
	return map[string]any{
		fieldEmoji:  map[string]string{fieldAlarm: alarmEmoji, "Info": "ℹ️"},
		fieldPrefix: "!",
	}
}
