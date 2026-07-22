package status

func cloneSystemStatsHistory(history []SystemStats) []SystemStats {
	if history == nil {
		return nil
	}
	cloned := make([]SystemStats, len(history))
	for i := range history {
		cloned[i] = cloneSystemStats(history[i])
	}
	return cloned
}

func cloneSystemStats(stats SystemStats) SystemStats {
	cloned := stats
	cloned.ServiceRuntime = cloneServiceRuntimeStats(stats.ServiceRuntime)
	return cloned
}

func cloneServiceRuntimeStats(stats []ServiceRuntimeStats) []ServiceRuntimeStats {
	if stats == nil {
		return nil
	}
	cloned := make([]ServiceRuntimeStats, len(stats))
	for i := range stats {
		cloned[i] = stats[i]
		if stats[i].Error != nil {
			errorText := *stats[i].Error
			cloned[i].Error = &errorText
		}
	}
	return cloned
}
