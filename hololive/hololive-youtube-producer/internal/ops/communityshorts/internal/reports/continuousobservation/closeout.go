package continuousobservation

import (
	"fmt"
	"strings"

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
)

const (
	closeoutScope          = "all_operational_channels"
	closeoutRule           = "observation status = finalized AND observation_window.internal_system_cause_posts == 0 (external_collection rows are excluded from pass/fail evaluation)"
	missingAlarmRule       = "observation status = finalized AND sent_history_dataset.missing_alarm_posts == 0"
	stateConsistencyRule   = "observation status = finalized AND sent_history_dataset.duplicate_sent_posts == 0 AND sent_history_dataset.missing_alarm_posts == 0"
	observationPeriodLabel = "observation_window"
)

func buildCloseout24H(
	observation Window,
	baseline communityshorts.TargetBaseline,
	sendCounts sendcounts.Report,
	latencyCause latencycause.Report,
) Closeout24H {
	closeout := newCloseout24H(observation, baseline, sendCounts)

	period, ok := findObservationLatencyCausePeriod(latencyCause)
	if !ok {
		return withMissingLatencyCause(closeout, observation.Status)
	}

	applyLatencyCausePeriod(&closeout, period)

	if observation.Status != StatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h closeout is pending until observation status becomes finalized; current observation_window internal_system_cause_posts=%d while excluded external_collection posts=%d remain logged across all operational channels.",
			closeout.InternalExceededPostCount,
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	if closeout.InternalExceededPostCount == 0 {
		closeout.Status = CloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=0; excluded external_collection posts=%d remain logged but do not affect pass/fail evaluation.",
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	closeout.Status = CloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=%d; excluded external_collection posts=%d remain outside pass/fail evaluation.",
		closeout.InternalExceededPostCount,
		closeout.ExcludedExternalExceededPostCount,
	)
	return closeout
}

func newCloseout24H(
	observation Window,
	baseline communityshorts.TargetBaseline,
	sendCounts sendcounts.Report,
) Closeout24H {
	closeout := Closeout24H{
		Status:             CloseoutStatusPending,
		AggregationScope:   closeoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		ObservedPostCount:  sendCounts.Summary.PostCount,
		Rule:               closeoutRule,
		Statement:          "24h closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func withMissingLatencyCause(
	closeout Closeout24H,
	status Status,
) Closeout24H {
	if status == StatusFinalized {
		closeout.Status = CloseoutStatusInsufficientEvidence
		closeout.Statement = "Finalized 24h closeout is blocked because the observation_window latency cause summary is missing."
	}
	return closeout
}

func applyLatencyCausePeriod(
	closeout *Closeout24H,
	period latencycause.PeriodView,
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

func buildMissingAlarmCloseout(
	observation Window,
	baseline communityshorts.TargetBaseline,
	dataset *alarmhistory.DatasetReport,
	datasetErr error,
) MissingAlarmCloseout {
	closeout := newMissingAlarmCloseout(observation, baseline)
	applyMissingAlarmDataset(&closeout, dataset)

	if observation.Status != StatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h missing-alarm closeout is pending until observation status becomes finalized; current missing_alarm_posts=%d across all operational channels.",
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		return withMissingAlarmDatasetMissing(closeout, datasetErr)
	}

	if closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded missing_alarm_posts=0 out of reference_posts=%d.",
			closeout.ReferencePostCount,
		)
		return closeout
	}

	closeout.Status = CloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded missing_alarm_posts=%d out of reference_posts=%d.",
		closeout.MissingAlarmPostCount,
		closeout.ReferencePostCount,
	)
	return closeout
}

func newMissingAlarmCloseout(
	observation Window,
	baseline communityshorts.TargetBaseline,
) MissingAlarmCloseout {
	closeout := MissingAlarmCloseout{
		Status:             CloseoutStatusPending,
		AggregationScope:   closeoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               missingAlarmRule,
		Statement:          "24h missing-alarm closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func applyMissingAlarmDataset(
	closeout *MissingAlarmCloseout,
	dataset *alarmhistory.DatasetReport,
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

func withMissingAlarmDatasetMissing(
	closeout MissingAlarmCloseout,
	datasetErr error,
) MissingAlarmCloseout {
	closeout.Status = CloseoutStatusInsufficientEvidence
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

func buildStateConsistencyCloseout(
	observation Window,
	baseline communityshorts.TargetBaseline,
	dataset *alarmhistory.DatasetReport,
	datasetErr error,
) StateConsistencyCloseout {
	closeout := newStateConsistencyCloseout(observation, baseline)
	applyStateConsistencyDataset(&closeout, dataset)

	if observation.Status != StatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h state-consistency closeout is pending until observation status becomes finalized; current duplicate_sent_posts=%d and missing_alarm_posts=%d across all operational channels.",
			closeout.DuplicateSentPostCount,
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		return withStateConsistencyDatasetMissing(closeout, datasetErr)
	}

	if closeout.DuplicateSentPostCount == 0 && closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CloseoutStatusPass
		closeout.Statement = "Finalized 24h observation across all operational channels recorded duplicate_sent_posts=0 and missing_alarm_posts=0; every reference post converged to a single completed sent state."
		return closeout
	}

	closeout.Status = CloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded duplicate_sent_posts=%d and missing_alarm_posts=%d; recovery did not converge all reference posts to a single completed sent state.",
		closeout.DuplicateSentPostCount,
		closeout.MissingAlarmPostCount,
	)
	return closeout
}

func newStateConsistencyCloseout(
	observation Window,
	baseline communityshorts.TargetBaseline,
) StateConsistencyCloseout {
	closeout := StateConsistencyCloseout{
		Status:             CloseoutStatusPending,
		AggregationScope:   closeoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               stateConsistencyRule,
		Statement:          "24h state-consistency closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	return closeout
}

func applyStateConsistencyDataset(
	closeout *StateConsistencyCloseout,
	dataset *alarmhistory.DatasetReport,
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

func withStateConsistencyDatasetMissing(
	closeout StateConsistencyCloseout,
	datasetErr error,
) StateConsistencyCloseout {
	closeout.Status = CloseoutStatusInsufficientEvidence
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

func findObservationLatencyCausePeriod(
	report latencycause.Report,
) (latencycause.PeriodView, bool) {
	for i := range report.Periods {
		if strings.TrimSpace(report.Periods[i].Summary.Label) == observationPeriodLabel {
			return report.Periods[i], true
		}
	}
	return latencycause.PeriodView{}, false
}
