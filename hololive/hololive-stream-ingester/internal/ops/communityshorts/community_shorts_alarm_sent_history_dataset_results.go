package communityshortsops

import (
	"sort"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type communityShortsAlarmSentHistoryDatasetComparisonAccumulator struct {
	BaselinePostCount                int
	SentPostCount                    int
	MatchedPostCount                 int
	UnsentPostCount                  int
	DuplicateSentPostCount           int
	UnexpectedSentPostCount          int
	IdentifierMismatchCandidateCount int
	MissingAlarmPostCount            int
}

var communityShortsAlarmSentHistoryDatasetVerdictAppliers = map[trackingrepo.ObservationPostComparisonVerdict]func(*communityShortsAlarmSentHistoryDatasetComparisonAccumulator){
	trackingrepo.ObservationPostComparisonVerdictMatched: func(accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator) {
		accumulator.MatchedPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictUnsent: func(accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator) {
		accumulator.UnsentPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictDuplicateSent: func(accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator) {
		accumulator.DuplicateSentPostCount++
	},
	trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate: func(accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator) {
		accumulator.IdentifierMismatchCandidateCount++
	},
}

func buildCommunityShortsAlarmSentHistoryDatasetResults(
	rows []CommunityShortsAlarmSentHistoryDatasetRow,
	verificationRows []CommunityShortsAlarmSentHistoryDatasetVerificationRow,
	referenceRows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	missingAlarmRows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
	summary CommunityShortsAlarmSentHistoryDatasetSummary,
	missingAlarmEvaluated bool,
) CommunityShortsAlarmSentHistoryDatasetResults {
	alarmTypeAccumulators := make(map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator)
	channelAccumulators := make(map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator)

	accumulateCommunityShortsAlarmSentHistoryDatasetRows(alarmTypeAccumulators, channelAccumulators, rows)
	accumulateCommunityShortsAlarmSentHistoryDatasetReferenceRows(alarmTypeAccumulators, channelAccumulators, referenceRows)
	accumulateCommunityShortsAlarmSentHistoryDatasetVerificationRows(alarmTypeAccumulators, channelAccumulators, verificationRows)
	accumulateCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(alarmTypeAccumulators, channelAccumulators, missingAlarmRows)

	results := buildCommunityShortsAlarmSentHistoryDatasetResultSummary(summary, missingAlarmEvaluated)
	results.AlarmTypeComparisons = buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeComparisons(alarmTypeAccumulators)
	results.ChannelComparisons = buildCommunityShortsAlarmSentHistoryDatasetChannelComparisons(channelAccumulators)

	return results
}

func accumulateCommunityShortsAlarmSentHistoryDatasetRows(
	alarmTypeAccumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	channelAccumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	rows []CommunityShortsAlarmSentHistoryDatasetRow,
) {
	for i := range rows {
		row := rows[i]
		incrementCommunityShortsAlarmSentHistoryDatasetSentPost(
			ensureCommunityShortsAlarmSentHistoryDatasetAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType),
		)
		incrementCommunityShortsAlarmSentHistoryDatasetSentPost(
			ensureCommunityShortsAlarmSentHistoryDatasetChannelAccumulator(channelAccumulators, row.ChannelID),
		)
	}
}

func accumulateCommunityShortsAlarmSentHistoryDatasetReferenceRows(
	alarmTypeAccumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	channelAccumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	referenceRows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
) {
	for i := range referenceRows {
		row := referenceRows[i]
		accumulateCommunityShortsAlarmSentHistoryDatasetReference(
			ensureCommunityShortsAlarmSentHistoryDatasetAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType),
			row.VerificationVerdict,
		)
		accumulateCommunityShortsAlarmSentHistoryDatasetReference(
			ensureCommunityShortsAlarmSentHistoryDatasetChannelAccumulator(channelAccumulators, row.ChannelID),
			row.VerificationVerdict,
		)
	}
}

func accumulateCommunityShortsAlarmSentHistoryDatasetVerificationRows(
	alarmTypeAccumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	channelAccumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	verificationRows []CommunityShortsAlarmSentHistoryDatasetVerificationRow,
) {
	for i := range verificationRows {
		row := verificationRows[i]
		if row.Verdict != trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
			continue
		}
		incrementCommunityShortsAlarmSentHistoryDatasetUnexpectedSent(
			ensureCommunityShortsAlarmSentHistoryDatasetAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType),
		)
		incrementCommunityShortsAlarmSentHistoryDatasetUnexpectedSent(
			ensureCommunityShortsAlarmSentHistoryDatasetChannelAccumulator(channelAccumulators, row.ChannelID),
		)
	}
}

func accumulateCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
	alarmTypeAccumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	channelAccumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	missingAlarmRows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
) {
	for i := range missingAlarmRows {
		row := missingAlarmRows[i]
		incrementCommunityShortsAlarmSentHistoryDatasetMissingAlarm(
			ensureCommunityShortsAlarmSentHistoryDatasetAlarmTypeAccumulator(alarmTypeAccumulators, row.AlarmType),
		)
		incrementCommunityShortsAlarmSentHistoryDatasetMissingAlarm(
			ensureCommunityShortsAlarmSentHistoryDatasetChannelAccumulator(channelAccumulators, row.ChannelID),
		)
	}
}

func buildCommunityShortsAlarmSentHistoryDatasetResultSummary(
	summary CommunityShortsAlarmSentHistoryDatasetSummary,
	missingAlarmEvaluated bool,
) CommunityShortsAlarmSentHistoryDatasetResults {
	return CommunityShortsAlarmSentHistoryDatasetResults{
		MissingAlarmEvaluated:     missingAlarmEvaluated,
		MissingAlarmPostCount:     summary.MissingAlarmPostCount,
		MissingSendStatePostCount: summary.MissingSendStatePostCount,
		AttemptedMissingPostCount: summary.AttemptedMissingPostCount,
		NotSentMissingPostCount:   summary.NotSentMissingPostCount,
		MissingAlarmZero:          missingAlarmEvaluated && summary.MissingAlarmPostCount == 0,
	}
}

func ensureCommunityShortsAlarmSentHistoryDatasetAlarmTypeAccumulator(
	accumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	alarmType domain.AlarmType,
) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
	if alarmType == "" {
		return nil
	}
	if existing, ok := accumulators[alarmType]; ok {
		return existing
	}
	accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
	accumulators[alarmType] = accumulator
	return accumulator
}

func ensureCommunityShortsAlarmSentHistoryDatasetChannelAccumulator(
	accumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	channelID string,
) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
	trimmed := strings.TrimSpace(channelID)
	if trimmed == "" {
		return nil
	}
	if existing, ok := accumulators[trimmed]; ok {
		return existing
	}
	accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
	accumulators[trimmed] = accumulator
	return accumulator
}

func incrementCommunityShortsAlarmSentHistoryDatasetSentPost(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
) {
	if accumulator != nil {
		accumulator.SentPostCount++
	}
}

func accumulateCommunityShortsAlarmSentHistoryDatasetReference(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	accumulator.BaselinePostCount++
	applyCommunityShortsAlarmSentHistoryDatasetVerdict(accumulator, verdict)
}

func incrementCommunityShortsAlarmSentHistoryDatasetUnexpectedSent(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
) {
	if accumulator != nil {
		accumulator.UnexpectedSentPostCount++
	}
}

func incrementCommunityShortsAlarmSentHistoryDatasetMissingAlarm(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
) {
	if accumulator != nil {
		accumulator.MissingAlarmPostCount++
	}
}

func buildCommunityShortsAlarmSentHistoryDatasetAlarmTypeComparisons(
	accumulators map[domain.AlarmType]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
) []CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison {
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
	comparisons := make([]CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison, 0, len(keys))
	for i := range keys {
		comparisons = append(comparisons, CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison{
			AlarmType:                        keys[i],
			BaselinePostCount:                accumulators[keys[i]].BaselinePostCount,
			SentPostCount:                    accumulators[keys[i]].SentPostCount,
			MatchedPostCount:                 accumulators[keys[i]].MatchedPostCount,
			UnsentPostCount:                  accumulators[keys[i]].UnsentPostCount,
			DuplicateSentPostCount:           accumulators[keys[i]].DuplicateSentPostCount,
			UnexpectedSentPostCount:          accumulators[keys[i]].UnexpectedSentPostCount,
			IdentifierMismatchCandidateCount: accumulators[keys[i]].IdentifierMismatchCandidateCount,
			MissingAlarmPostCount:            accumulators[keys[i]].MissingAlarmPostCount,
		})
	}
	return comparisons
}

func buildCommunityShortsAlarmSentHistoryDatasetChannelComparisons(
	accumulators map[string]*communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
) []CommunityShortsAlarmSentHistoryDatasetChannelComparison {
	if len(accumulators) == 0 {
		return nil
	}
	keys := make([]string, 0, len(accumulators))
	for channelID := range accumulators {
		keys = append(keys, channelID)
	}
	sort.Strings(keys)
	comparisons := make([]CommunityShortsAlarmSentHistoryDatasetChannelComparison, 0, len(keys))
	for i := range keys {
		comparisons = append(comparisons, CommunityShortsAlarmSentHistoryDatasetChannelComparison{
			ChannelID:                        keys[i],
			BaselinePostCount:                accumulators[keys[i]].BaselinePostCount,
			SentPostCount:                    accumulators[keys[i]].SentPostCount,
			MatchedPostCount:                 accumulators[keys[i]].MatchedPostCount,
			UnsentPostCount:                  accumulators[keys[i]].UnsentPostCount,
			DuplicateSentPostCount:           accumulators[keys[i]].DuplicateSentPostCount,
			UnexpectedSentPostCount:          accumulators[keys[i]].UnexpectedSentPostCount,
			IdentifierMismatchCandidateCount: accumulators[keys[i]].IdentifierMismatchCandidateCount,
			MissingAlarmPostCount:            accumulators[keys[i]].MissingAlarmPostCount,
		})
	}
	return comparisons
}

func applyCommunityShortsAlarmSentHistoryDatasetVerdict(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	if applyVerdict, ok := communityShortsAlarmSentHistoryDatasetVerdictAppliers[verdict]; ok {
		applyVerdict(accumulator)
	}
}
