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

import (
	"maps"
	"slices"
)

var templateSampleData = mergeTemplateSampleData(
	templateSampleCoreData(),
	templateSampleMajorEventData(),
	templateSampleMemberNewsData(),
)

func mergeTemplateSampleData(groups ...map[TemplateKey]any) map[TemplateKey]any {
	merged := make(map[TemplateKey]any)
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

func GetTemplateSampleData(key TemplateKey) any {
	return templateSampleData[key]
}

func GetAllTemplateKeys() []TemplateKey {
	return []TemplateKey{
		TemplateKeyOutboxShorts,
		TemplateKeyOutboxCommunity,
		TemplateKeyOutboxVideo,
		TemplateKeyOutboxMilestone,
		TemplateKeyOutboxVideoGroup,
		TemplateKeyOutboxShortsGroup,
		TemplateKeyOutboxCommunityGroup,
		TemplateKeyCmdAlarmList,
		TemplateKeyCmdAlarmNotification,
		TemplateKeyCmdAlarmLiveStarted,
		TemplateKeyCmdLiveStreams,
		TemplateKeyCmdUpcomingStreams,
		TemplateKeyCmdHelp,
		TemplateKeyCmdMemberDirectory,
		TemplateKeyCmdChannelSchedule,
		TemplateKeyCmdAlarmAdded,
		TemplateKeyCmdAlarmRemoved,
		TemplateKeyCmdAlarmCleared,
		TemplateKeyCmdMilestoneAchieved,
		TemplateKeyCmdMilestoneApproach,
		TemplateKeyCmdMajorEventWeeklySummary,
		TemplateKeyCmdMajorEventMonthlySummary,
		TemplateKeyCmdMajorEventSubscribed,
		TemplateKeyCmdMajorEventUnsubscribed,
		TemplateKeyCmdMajorEventAlreadySub,
		TemplateKeyCmdMajorEventNotSub,
		TemplateKeyCmdMajorEventStatus,
		TemplateKeyCmdMajorEventUsage,
		TemplateKeyCmdMemberNewsDigest,
		TemplateKeyCmdMemberNewsNoMembers,
		TemplateKeyCmdMemberNewsSubscribed,
		TemplateKeyCmdMemberNewsUnsubscribed,
		TemplateKeyCmdMemberNewsAlreadySub,
		TemplateKeyCmdMemberNewsNotSub,
		TemplateKeyCmdMemberNewsStatus,
	}
}

func IsValidTemplateKey(key TemplateKey) bool {
	return slices.Contains(GetAllTemplateKeys(), key)
}
