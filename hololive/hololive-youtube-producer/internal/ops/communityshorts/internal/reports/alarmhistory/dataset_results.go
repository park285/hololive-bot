package alarmhistory

import (
	"sort"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type comparisonAccumulator struct {
	BaselinePostCount                int
	SentPostCount                    int
	MatchedPostCount                 int
	UnsentPostCount                  int
	DuplicateSentPostCount           int
	UnexpectedSentPostCount          int
	IdentifierMismatchCandidateCount int
	MissingAlarmPostCount            int
}

var verdictAppliers = map[trackingrepo.ObservationPostComparisonVerdict]func(*comparisonAccumulator){
	trackingrepo.ObservationPostComparisonVerdictMatched: func(a *comparisonAccumulator) {
		a.MatchedPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictUnsent: func(a *comparisonAccumulator) {
		a.UnsentPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictDuplicateSent: func(a *comparisonAccumulator) {
		a.DuplicateSentPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate: func(a *comparisonAccumulator) {
		a.IdentifierMismatchCandidateCount++
	},
}

func buildDatasetResults(
	rows []DatasetRow,
	verificationRows []DatasetVerificationRow,
	referenceRows []DatasetReferenceRow,
	missingAlarmRows []DatasetMissingAlarmRow,
	summary DatasetSummary,
	missingAlarmEvaluated bool,
) DatasetResults {
	alarmTypeAccumulators := make(map[domain.AlarmType]*comparisonAccumulator)
	channelAccumulators := make(map[string]*comparisonAccumulator)

	accumulateDatasetRows(alarmTypeAccumulators, channelAccumulators, rows)
	accumulateReferenceRows(alarmTypeAccumulators, channelAccumulators, referenceRows)
	accumulateVerificationRows(alarmTypeAccumulators, channelAccumulators, verificationRows)
	accumulateMissingAlarmRows(alarmTypeAccumulators, channelAccumulators, missingAlarmRows)

	results := buildResultSummary(summary, missingAlarmEvaluated)
	results.AlarmTypeComparisons = buildAlarmTypeComparisons(alarmTypeAccumulators)
	results.ChannelComparisons = buildChannelComparisons(channelAccumulators)

	return results
}

func accumulateDatasetRows(
	alarmTypeAccumulators map[domain.AlarmType]*comparisonAccumulator,
	channelAccumulators map[string]*comparisonAccumulator,
	rows []DatasetRow,
) {
	for i := range rows {
		row := rows[i]
		incrementSentPost(ensureAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType))
		incrementSentPost(ensureChannelAccumulator(channelAccumulators, row.ChannelID))
	}
}

func accumulateReferenceRows(
	alarmTypeAccumulators map[domain.AlarmType]*comparisonAccumulator,
	channelAccumulators map[string]*comparisonAccumulator,
	referenceRows []DatasetReferenceRow,
) {
	for i := range referenceRows {
		row := referenceRows[i]
		accumulateReference(ensureAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType), row.VerificationVerdict)
		accumulateReference(ensureChannelAccumulator(channelAccumulators, row.ChannelID), row.VerificationVerdict)
	}
}

func accumulateVerificationRows(
	alarmTypeAccumulators map[domain.AlarmType]*comparisonAccumulator,
	channelAccumulators map[string]*comparisonAccumulator,
	verificationRows []DatasetVerificationRow,
) {
	for i := range verificationRows {
		row := verificationRows[i]
		if row.Verdict != trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
			continue
		}
		incrementUnexpectedSent(ensureAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType))
		incrementUnexpectedSent(ensureChannelAccumulator(channelAccumulators, row.ChannelID))
	}
}

func accumulateMissingAlarmRows(
	alarmTypeAccumulators map[domain.AlarmType]*comparisonAccumulator,
	channelAccumulators map[string]*comparisonAccumulator,
	missingAlarmRows []DatasetMissingAlarmRow,
) {
	for i := range missingAlarmRows {
		row := missingAlarmRows[i]
		incrementMissingAlarm(ensureAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType))
		incrementMissingAlarm(ensureChannelAccumulator(channelAccumulators, row.ChannelID))
	}
}

func buildResultSummary(
	summary DatasetSummary,
	missingAlarmEvaluated bool,
) DatasetResults {
	return DatasetResults{
		MissingAlarmEvaluated:     missingAlarmEvaluated,
		MissingAlarmPostCount:     summary.MissingAlarmPostCount,
		MissingSendStatePostCount: summary.MissingSendStatePostCount,
		AttemptedMissingPostCount: summary.AttemptedMissingPostCount,
		NotSentMissingPostCount:   summary.NotSentMissingPostCount,
		MissingAlarmZero:          missingAlarmEvaluated && summary.MissingAlarmPostCount == 0,
	}
}

func ensureAlarmTypeAccumulator(
	accumulators map[domain.AlarmType]*comparisonAccumulator,
	alarmType domain.AlarmType,
) *comparisonAccumulator {
	if alarmType == "" {
		return nil
	}
	if existing, ok := accumulators[alarmType]; ok {
		return existing
	}
	accumulator := &comparisonAccumulator{}
	accumulators[alarmType] = accumulator
	return accumulator
}

func ensureChannelAccumulator(
	accumulators map[string]*comparisonAccumulator,
	channelID string,
) *comparisonAccumulator {
	trimmed := strings.TrimSpace(channelID)
	if trimmed == "" {
		return nil
	}
	if existing, ok := accumulators[trimmed]; ok {
		return existing
	}
	accumulator := &comparisonAccumulator{}
	accumulators[trimmed] = accumulator
	return accumulator
}

func incrementSentPost(accumulator *comparisonAccumulator) {
	if accumulator != nil {
		accumulator.SentPostCount++
	}
}

func accumulateReference(
	accumulator *comparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	accumulator.BaselinePostCount++
	applyVerdict(accumulator, verdict)
}

func incrementUnexpectedSent(accumulator *comparisonAccumulator) {
	if accumulator != nil {
		accumulator.UnexpectedSentPostCount++
	}
}

func incrementMissingAlarm(accumulator *comparisonAccumulator) {
	if accumulator != nil {
		accumulator.MissingAlarmPostCount++
	}
}

func buildAlarmTypeComparisons(
	accumulators map[domain.AlarmType]*comparisonAccumulator,
) []DatasetAlarmTypeComparison {
	if len(accumulators) == 0 {
		return nil
	}
	keys := make([]domain.AlarmType, 0, len(accumulators))
	for alarmType := range accumulators {
		keys = append(keys, alarmType)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	comparisons := make([]DatasetAlarmTypeComparison, 0, len(keys))
	for i := range keys {
		a := accumulators[keys[i]]
		comparisons = append(comparisons, DatasetAlarmTypeComparison{
			AlarmType:                        keys[i],
			BaselinePostCount:                a.BaselinePostCount,
			SentPostCount:                    a.SentPostCount,
			MatchedPostCount:                 a.MatchedPostCount,
			UnsentPostCount:                  a.UnsentPostCount,
			DuplicateSentPostCount:           a.DuplicateSentPostCount,
			UnexpectedSentPostCount:          a.UnexpectedSentPostCount,
			IdentifierMismatchCandidateCount: a.IdentifierMismatchCandidateCount,
			MissingAlarmPostCount:            a.MissingAlarmPostCount,
		})
	}
	return comparisons
}

func buildChannelComparisons(
	accumulators map[string]*comparisonAccumulator,
) []DatasetChannelComparison {
	if len(accumulators) == 0 {
		return nil
	}
	keys := make([]string, 0, len(accumulators))
	for channelID := range accumulators {
		keys = append(keys, channelID)
	}
	sort.Strings(keys)
	comparisons := make([]DatasetChannelComparison, 0, len(keys))
	for i := range keys {
		a := accumulators[keys[i]]
		comparisons = append(comparisons, DatasetChannelComparison{
			ChannelID:                        keys[i],
			BaselinePostCount:                a.BaselinePostCount,
			SentPostCount:                    a.SentPostCount,
			MatchedPostCount:                 a.MatchedPostCount,
			UnsentPostCount:                  a.UnsentPostCount,
			DuplicateSentPostCount:           a.DuplicateSentPostCount,
			UnexpectedSentPostCount:          a.UnexpectedSentPostCount,
			IdentifierMismatchCandidateCount: a.IdentifierMismatchCandidateCount,
			MissingAlarmPostCount:            a.MissingAlarmPostCount,
		})
	}
	return comparisons
}

func applyVerdict(
	accumulator *comparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	if fn, ok := verdictAppliers[verdict]; ok {
		fn(accumulator)
	}
}
