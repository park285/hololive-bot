package youtube

import "testing"

func TestStatsRepository_InterfaceContracts(t *testing.T) {
	t.Run("domain-split interfaces", func(t *testing.T) {
		var _ StatsWriteRepository = (*StatsRepository)(nil)
		var _ StatsReadRepository = (*StatsRepository)(nil)
		var _ MilestoneRepository = (*StatsRepository)(nil)
		var _ SubscriberGraphRepository = (*StatsRepository)(nil)
		var _ NotificationRepository = (*StatsRepository)(nil)
	})

	t.Run("consumer-minimal interfaces", func(t *testing.T) {
		var _ StatsServiceRepository = (*StatsRepository)(nil)
		var _ StatsSchedulerRepository = (*StatsRepository)(nil)
		var _ StatsCommandRepository = (*StatsRepository)(nil)
		var _ StatsDashboardRepository = (*StatsRepository)(nil)
	})
}
