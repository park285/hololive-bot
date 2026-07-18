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

import (
	"maps"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var templateSampleData = mergeTemplateSampleData(
	templateSampleCoreData(),
	templateSampleMajorEventData(),
	templateSampleMemberNewsData(),
)

func mergeTemplateSampleData(groups ...map[domain.TemplateKey]any) map[domain.TemplateKey]any {
	merged := make(map[domain.TemplateKey]any)
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

func GetTemplateSampleData(key domain.TemplateKey) any {
	return templateSampleData[key]
}

func GetAllTemplateKeys() []domain.TemplateKey {
	return []domain.TemplateKey{
		domain.TemplateKeyOutboxShorts,
		domain.TemplateKeyOutboxCommunity,
		domain.TemplateKeyOutboxVideo,
		domain.TemplateKeyOutboxMilestone,
		domain.TemplateKeyOutboxVideoGroup,
		domain.TemplateKeyOutboxShortsGroup,
		domain.TemplateKeyOutboxCommunityGroup,
		domain.TemplateKeyCmdAlarmList,
		domain.TemplateKeyCmdAlarmNotification,
		domain.TemplateKeyCmdAlarmLiveStarted,
		domain.TemplateKeyCmdAlarmNotificationGroup,
		domain.TemplateKeyCmdLiveStreams,
		domain.TemplateKeyCmdUpcomingStreams,
		domain.TemplateKeyCmdHelp,
		domain.TemplateKeyCmdMemberDirectory,
		domain.TemplateKeyCmdProfile,
		domain.TemplateKeyCmdChannelSchedule,
		domain.TemplateKeyCmdAlarmAdded,
		domain.TemplateKeyCmdAlarmRemoved,
		domain.TemplateKeyCmdAlarmCleared,
		domain.TemplateKeyCmdMilestoneAchieved,
		domain.TemplateKeyCmdMilestoneApproach,
		domain.TemplateKeyCmdStatsCount,
		domain.TemplateKeyCmdStatsGainers,
		domain.TemplateKeyCmdCalendar,
		domain.TemplateKeyCmdMemberNotLive,
		domain.TemplateKeyCmdMemberNoUpcoming,
		domain.TemplateKeyCmdMemberNotFound,
		domain.TemplateKeyCmdAmbiguousMember,
		domain.TemplateKeyCelebrationBirthday,
		domain.TemplateKeyCelebrationAnniversary,
		domain.TemplateKeyCelebrationBirthdayStream,
		domain.TemplateKeyAlarmDispatchNotification,
		domain.TemplateKeyAlarmDispatchNotificationGroup,
		domain.TemplateKeyCmdMajorEventWeeklySummary,
		domain.TemplateKeyCmdMajorEventMonthlySummary,
		domain.TemplateKeyCmdMajorEventSubscribed,
		domain.TemplateKeyCmdMajorEventUnsubscribed,
		domain.TemplateKeyCmdMajorEventAlreadySub,
		domain.TemplateKeyCmdMajorEventNotSub,
		domain.TemplateKeyCmdMajorEventStatus,
		domain.TemplateKeyCmdMajorEventUsage,
		domain.TemplateKeyCmdMemberNewsDigest,
		domain.TemplateKeyCmdMemberNewsNoMembers,
		domain.TemplateKeyCmdMemberNewsSubscribed,
		domain.TemplateKeyCmdMemberNewsUnsubscribed,
		domain.TemplateKeyCmdMemberNewsAlreadySub,
		domain.TemplateKeyCmdMemberNewsNotSub,
		domain.TemplateKeyCmdMemberNewsStatus,
	}
}

func IsValidTemplateKey(key domain.TemplateKey) bool {
	return slices.Contains(GetAllTemplateKeys(), key)
}
