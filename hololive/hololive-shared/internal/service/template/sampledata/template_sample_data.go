package sampledata

import (
	"maps"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var templateSampleData = mergeTemplateSampleData(
	templateSampleCoreData(),
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

var allTemplateKeySet = buildTemplateKeySet(allTemplateKeys)

func buildTemplateKeySet(keys []domain.TemplateKey) map[domain.TemplateKey]struct{} {
	set := make(map[domain.TemplateKey]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}

	return set
}

func GetAllTemplateKeys() []domain.TemplateKey {
	return slices.Clone(allTemplateKeys)
}

var allTemplateKeys = []domain.TemplateKey{
	domain.TemplateKeyOutboxShorts,
	domain.TemplateKeyOutboxCommunity,
	domain.TemplateKeyOutboxVideo,
	domain.TemplateKeyOutboxVideoGroup,
	domain.TemplateKeyOutboxShortsGroup,
	domain.TemplateKeyOutboxCommunityGroup,
	domain.TemplateKeyCmdAlarmList,
	domain.TemplateKeyCmdLiveStreams,
	domain.TemplateKeyCmdUpcomingStreams,
	domain.TemplateKeyCmdHelp,
	domain.TemplateKeyCmdMemberDirectory,
	domain.TemplateKeyCmdProfile,
	domain.TemplateKeyCmdChannelSchedule,
	domain.TemplateKeyCmdAlarmAdded,
	domain.TemplateKeyCmdAlarmRemoved,
	domain.TemplateKeyCmdAlarmCleared,
	domain.TemplateKeyCmdCalendar,
	domain.TemplateKeyCmdMemberNotLive,
	domain.TemplateKeyCmdMemberNoUpcoming,
	domain.TemplateKeyCmdMemberNotFound,
	domain.TemplateKeyCmdAmbiguousMember,
	domain.TemplateKeyCelebrationBirthday,
	domain.TemplateKeyCelebrationAnniversary,
	domain.TemplateKeyAlarmDispatchNotification,
	domain.TemplateKeyAlarmDispatchNotificationGroup,
	domain.TemplateKeyXSpaceStarted,
}

func IsValidTemplateKey(key domain.TemplateKey) bool {
	_, ok := allTemplateKeySet[key]
	return ok
}
