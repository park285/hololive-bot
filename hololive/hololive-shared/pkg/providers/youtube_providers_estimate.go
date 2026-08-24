package providers

import "strings"

func estimatedRegistrationRequestUnitsPerRun(registration *ChannelPollerRegistration) float64 {
	if registration == nil {
		return 0
	}

	requests := registration.RequestsPerRun
	if requests <= 0 {
		requests = 1
	}

	return float64(requests)
}

func estimatedRegistrationWorstCaseRequestUnitsPerRun(registration *ChannelPollerRegistration) float64 {
	if registration == nil {
		return 0
	}

	if registration.WorstCaseRequestUnitsPerRun > 0 {
		return registration.WorstCaseRequestUnitsPerRun
	}

	attempts := registration.WorstCaseAttempts
	if attempts <= 0 {
		attempts = 1
	}

	return estimatedRegistrationRequestUnitsPerRun(registration) * float64(attempts)
}

func estimatedRegistrationRPM(registration *ChannelPollerRegistration, targetCount int) float64 {
	if registration == nil {
		return 0
	}

	if registration.Interval <= 0 || targetCount <= 0 {
		return 0
	}

	return float64(targetCount) * (60.0 / registration.Interval.Seconds()) * estimatedRegistrationRequestUnitsPerRun(registration)
}

func estimatedRegistrationWorstCaseRPM(registration *ChannelPollerRegistration, targetCount int) float64 {
	if registration == nil {
		return 0
	}

	if registration.Interval <= 0 || targetCount <= 0 {
		return 0
	}

	return float64(targetCount) * (60.0 / registration.Interval.Seconds()) * estimatedRegistrationWorstCaseRequestUnitsPerRun(registration)
}

func estimatedRequestsPerMinute(registrations []ChannelPollerRegistration) float64 {
	var rpm float64

	for i := range registrations {
		registration := &registrations[i]
		targetCount := 1

		if registration.HasExplicitChannelIDs {
			targetCount = len(uniqueChannelIDs(registration.ChannelIDs))
		}

		rpm += estimatedRegistrationRPM(registration, targetCount)
	}

	return rpm
}

func uniqueChannelIDs(channelIDs []string) []string {
	if len(channelIDs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(channelIDs))
	unique := make([]string, 0, len(channelIDs))

	for _, channelID := range channelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			continue
		}

		if _, exists := seen[channelID]; exists {
			continue
		}

		seen[channelID] = struct{}{}
		unique = append(unique, channelID)
	}

	return unique
}
