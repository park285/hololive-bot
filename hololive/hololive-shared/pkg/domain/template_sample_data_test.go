package domain_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestTemplateSampleData_AllKeysPresent(t *testing.T) {
	allKeys := []domain.TemplateKey{
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
		domain.TemplateKeyCmdLiveStreams,
		domain.TemplateKeyCmdUpcomingStreams,
		domain.TemplateKeyCmdHelp,
		domain.TemplateKeyCmdMemberDirectory,
		domain.TemplateKeyCmdChannelSchedule,
		domain.TemplateKeyCmdAlarmAdded,
		domain.TemplateKeyCmdAlarmRemoved,
		domain.TemplateKeyCmdAlarmCleared,
		domain.TemplateKeyCmdMilestoneAchieved,
		domain.TemplateKeyCmdMilestoneApproach,
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

	for _, key := range allKeys {
		t.Run(string(key), func(t *testing.T) {
			data := domain.GetTemplateSampleData(key)
			assert.NotNil(t, data, "sample data should exist for key %s", key)
		})
	}
}

func TestTemplateSampleData_OutboxTypes(t *testing.T) {
	tests := []struct {
		key           domain.TemplateKey
		requiredField string
	}{
		{domain.TemplateKeyOutboxShorts, "MemberName"},
		{domain.TemplateKeyOutboxCommunity, "ContentText"},
		{domain.TemplateKeyOutboxVideo, "Title"},
		{domain.TemplateKeyOutboxMilestone, "Milestone"},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			data := domain.GetTemplateSampleData(tt.key)
			m, ok := data.(map[string]any)
			assert.True(t, ok, "outbox data should be map[string]any")
			assert.Contains(t, m, tt.requiredField)
		})
	}
}
