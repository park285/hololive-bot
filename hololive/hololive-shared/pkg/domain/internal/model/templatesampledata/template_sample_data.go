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

package templatesampledata

import (
	"github.com/kapu/hololive-shared/pkg/domain/internal/model"
	"maps"
)

import "slices"

var templateSampleData = mergeTemplateSampleData(
	templateSampleCoreData(),
	templateSampleMajorEventData(),
	templateSampleMemberNewsData(),
)

func mergeTemplateSampleData(groups ...map[model.TemplateKey]any) map[model.TemplateKey]any {
	merged := make(map[model.TemplateKey]any)
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

func GetTemplateSampleData(key model.TemplateKey) any {
	return templateSampleData[key]
}

func GetAllTemplateKeys() []model.TemplateKey {
	return []model.TemplateKey{
		model.TemplateKeyOutboxShorts,
		model.TemplateKeyOutboxCommunity,
		model.TemplateKeyOutboxVideo,
		model.TemplateKeyOutboxMilestone,
		model.TemplateKeyOutboxVideoGroup,
		model.TemplateKeyOutboxShortsGroup,
		model.TemplateKeyOutboxCommunityGroup,
		model.TemplateKeyCmdAlarmList,
		model.TemplateKeyCmdAlarmNotification,
		model.TemplateKeyCmdAlarmLiveStarted,
		model.TemplateKeyCmdLiveStreams,
		model.TemplateKeyCmdUpcomingStreams,
		model.TemplateKeyCmdHelp,
		model.TemplateKeyCmdMemberDirectory,
		model.TemplateKeyCmdChannelSchedule,
		model.TemplateKeyCmdAlarmAdded,
		model.TemplateKeyCmdAlarmRemoved,
		model.TemplateKeyCmdAlarmCleared,
		model.TemplateKeyCmdMilestoneAchieved,
		model.TemplateKeyCmdMilestoneApproach,
		model.TemplateKeyCmdMajorEventWeeklySummary,
		model.TemplateKeyCmdMajorEventMonthlySummary,
		model.TemplateKeyCmdMajorEventSubscribed,
		model.TemplateKeyCmdMajorEventUnsubscribed,
		model.TemplateKeyCmdMajorEventAlreadySub,
		model.TemplateKeyCmdMajorEventNotSub,
		model.TemplateKeyCmdMajorEventStatus,
		model.TemplateKeyCmdMajorEventUsage,
		model.TemplateKeyCmdMemberNewsDigest,
		model.TemplateKeyCmdMemberNewsNoMembers,
		model.TemplateKeyCmdMemberNewsSubscribed,
		model.TemplateKeyCmdMemberNewsUnsubscribed,
		model.TemplateKeyCmdMemberNewsAlreadySub,
		model.TemplateKeyCmdMemberNewsNotSub,
		model.TemplateKeyCmdMemberNewsStatus,
	}
}

func IsValidTemplateKey(key model.TemplateKey) bool {
	return slices.Contains(GetAllTemplateKeys(), key)
}
