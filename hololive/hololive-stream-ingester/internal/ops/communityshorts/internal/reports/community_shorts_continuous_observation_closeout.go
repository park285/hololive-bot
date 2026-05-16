package reports

import (
	"fmt"
	"strings"

	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

const (
	communityShortsContinuousObservationCloseoutScope        = "all_operational_channels"
	communityShortsContinuousObservationCloseoutRule         = "observation status = finalized AND observation_window.internal_system_cause_posts == 0 (external_collection rows are excluded from pass/fail evaluation)"
	communityShortsContinuousObservationMissingAlarmRule     = "observation status = finalized AND sent_history_dataset.missing_alarm_posts == 0"
	communityShortsContinuousObservationStateConsistencyRule = "observation status = finalized AND sent_history_dataset.duplicate_sent_posts == 0 AND sent_history_dataset.missing_alarm_posts == 0"
)

func buildCommunityShortsContinuousObservation24HCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
	sendCounts CommunityShortsSendCountReport,
	latencyCause CommunityShortsLatencyCauseReport,
) CommunityShortsContinuousObservation24HCloseout {
	closeout := newCommunityShortsContinuousObservation24HCloseout(observation, baseline, sendCounts)

	period, ok := findCommunityShortsObservationLatencyCausePeriod(latencyCause)
	if !ok {
		return withCommunityShortsContinuousObservationMissingLatencyCause(closeout, observation.Status)
	}

	applyCommunityShortsContinuousObservationLatencyCausePeriod(&closeout, period)

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h closeout is pending until observation status becomes finalized; current observation_window internal_system_cause_posts=%d while excluded external_collection posts=%d remain logged across all operational channels.",
			closeout.InternalExceededPostCount,
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	if closeout.InternalExceededPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=0; excluded external_collection posts=%d remain logged but do not affect pass/fail evaluation.",
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=%d; excluded external_collection posts=%d remain outside pass/fail evaluation.",
		closeout.InternalExceededPostCount,
		closeout.ExcludedExternalExceededPostCount,
	)
	return closeout
}

func newCommunityShortsContinuousObservation24HCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
	sendCounts CommunityShortsSendCountReport,
) CommunityShortsContinuousObservation24HCloseout {
	closeout := CommunityShortsContinuousObservation24HCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		ObservedPostCount:  sendCounts.Summary.PostCount,
		Rule:               communityShortsContinuousObservationCloseoutRule,
		Statement:          "24h closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func withCommunityShortsContinuousObservationMissingLatencyCause(
	closeout CommunityShortsContinuousObservation24HCloseout,
	status CommunityShortsContinuousObservationStatus,
) CommunityShortsContinuousObservation24HCloseout {
	if status == CommunityShortsContinuousObservationStatusFinalized {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
		closeout.Statement = "Finalized 24h closeout is blocked because the observation_window latency cause summary is missing."
	}
	return closeout
}

func applyCommunityShortsContinuousObservationLatencyCausePeriod(
	closeout *CommunityShortsContinuousObservation24HCloseout,
	period CommunityShortsLatencyCausePeriodView,
) {
	closeout.ObservationPeriodLabel = strings.TrimSpace(period.Summary.Label)
	closeout.TotalExceededPostCount = period.CauseSummary.ExceededPostCount
	closeout.InternalExceededPostCount = period.CauseSummary.InternalSystemCausePostCount
	closeout.NonInternalExceededPostCount = period.CauseSummary.NonInternalSystemCausePostCount
	closeout.ExcludedExternalExceededPostCount = period.CauseSummary.ExcludedExternalDelayPostCount
	if closeout.ExcludedExternalExceededPostCount == 0 && period.CauseSummary.ExternalCollectionSourcePostCount > 0 {
		closeout.ExcludedExternalExceededPostCount = period.CauseSummary.ExternalCollectionSourcePostCount
	}
}

func buildCommunityShortsContinuousObservationMissingAlarmCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
	datasetErr error,
) CommunityShortsContinuousObservationMissingAlarmCloseout {
	closeout := newCommunityShortsContinuousObservationMissingAlarmCloseout(observation, baseline)
	applyCommunityShortsContinuousObservationMissingAlarmDataset(&closeout, dataset)

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h missing-alarm closeout is pending until observation status becomes finalized; current missing_alarm_posts=%d across all operational channels.",
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		return withCommunityShortsContinuousObservationMissingAlarmDatasetMissing(closeout, datasetErr)
	}

	if closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded missing_alarm_posts=0 out of reference_posts=%d.",
			closeout.ReferencePostCount,
		)
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded missing_alarm_posts=%d out of reference_posts=%d.",
		closeout.MissingAlarmPostCount,
		closeout.ReferencePostCount,
	)
	return closeout
}

func newCommunityShortsContinuousObservationMissingAlarmCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
) CommunityShortsContinuousObservationMissingAlarmCloseout {
	closeout := CommunityShortsContinuousObservationMissingAlarmCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               communityShortsContinuousObservationMissingAlarmRule,
		Statement:          "24h missing-alarm closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func applyCommunityShortsContinuousObservationMissingAlarmDataset(
	closeout *CommunityShortsContinuousObservationMissingAlarmCloseout,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
) {
	if dataset == nil {
		return
	}
	closeout.ReferencePostCount = dataset.Summary.ReferenceRowCount
	closeout.SendStatePostCount = dataset.Summary.SendStatePostCount
	closeout.MissingAlarmPostCount = dataset.Summary.MissingAlarmPostCount
	closeout.MissingSendStatePostCount = dataset.Summary.MissingSendStatePostCount
	closeout.AttemptedMissingPostCount = dataset.Summary.AttemptedMissingPostCount
	closeout.NotSentMissingPostCount = dataset.Summary.NotSentMissingPostCount
}

func withCommunityShortsContinuousObservationMissingAlarmDatasetMissing(
	closeout CommunityShortsContinuousObservationMissingAlarmCloseout,
	datasetErr error,
) CommunityShortsContinuousObservationMissingAlarmCloseout {
	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
	if datasetErr != nil {
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h missing-alarm closeout is blocked because the sent-history dataset could not be collected: %v.",
			datasetErr,
		)
	} else {
		closeout.Statement = "Finalized 24h missing-alarm closeout is blocked because the sent-history dataset is missing."
	}
	return closeout
}

func buildCommunityShortsContinuousObservationStateConsistencyCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
	datasetErr error,
) CommunityShortsContinuousObservationStateConsistencyCloseout {
	closeout := newCommunityShortsContinuousObservationStateConsistencyCloseout(observation, baseline)
	applyCommunityShortsContinuousObservationStateConsistencyDataset(&closeout, dataset)

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h state-consistency closeout is pending until observation status becomes finalized; current duplicate_sent_posts=%d and missing_alarm_posts=%d across all operational channels.",
			closeout.DuplicateSentPostCount,
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		return withCommunityShortsContinuousObservationStateConsistencyDatasetMissing(closeout, datasetErr)
	}

	if closeout.DuplicateSentPostCount == 0 && closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = "Finalized 24h observation across all operational channels recorded duplicate_sent_posts=0 and missing_alarm_posts=0; every reference post converged to a single completed sent state."
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded duplicate_sent_posts=%d and missing_alarm_posts=%d; recovery did not converge all reference posts to a single completed sent state.",
		closeout.DuplicateSentPostCount,
		closeout.MissingAlarmPostCount,
	)
	return closeout
}

func newCommunityShortsContinuousObservationStateConsistencyCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline communityshorts.TargetBaseline,
) CommunityShortsContinuousObservationStateConsistencyCloseout {
	closeout := CommunityShortsContinuousObservationStateConsistencyCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               communityShortsContinuousObservationStateConsistencyRule,
		Statement:          "24h state-consistency closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func applyCommunityShortsContinuousObservationStateConsistencyDataset(
	closeout *CommunityShortsContinuousObservationStateConsistencyCloseout,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
) {
	if dataset == nil {
		return
	}
	closeout.ReferencePostCount = dataset.Summary.ReferenceRowCount
	closeout.SendStatePostCount = dataset.Summary.SendStatePostCount
	closeout.DuplicateSentPostCount = dataset.Summary.DuplicateSentPostCount
	closeout.MissingAlarmPostCount = dataset.Summary.MissingAlarmPostCount
	closeout.MissingSendStatePostCount = dataset.Summary.MissingSendStatePostCount
	closeout.AttemptedMissingPostCount = dataset.Summary.AttemptedMissingPostCount
	closeout.NotSentMissingPostCount = dataset.Summary.NotSentMissingPostCount
}

func withCommunityShortsContinuousObservationStateConsistencyDatasetMissing(
	closeout CommunityShortsContinuousObservationStateConsistencyCloseout,
	datasetErr error,
) CommunityShortsContinuousObservationStateConsistencyCloseout {
	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
	if datasetErr != nil {
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h state-consistency closeout is blocked because the sent-history dataset could not be collected: %v.",
			datasetErr,
		)
	} else {
		closeout.Statement = "Finalized 24h state-consistency closeout is blocked because the sent-history dataset is missing."
	}
	return closeout
}

func findCommunityShortsObservationLatencyCausePeriod(
	report CommunityShortsLatencyCauseReport,
) (CommunityShortsLatencyCausePeriodView, bool) {
	for i := range report.Periods {
		if strings.TrimSpace(report.Periods[i].Summary.Label) == communityShortsLatencyCauseObservationPeriodLabel {
			return report.Periods[i], true
		}
	}
	return CommunityShortsLatencyCausePeriodView{}, false
}
