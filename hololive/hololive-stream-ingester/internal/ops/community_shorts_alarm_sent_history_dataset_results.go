package ops

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

	ensureAlarmTypeAccumulator := func(alarmType domain.AlarmType) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
		if alarmType == "" {
			return nil
		}
		if existing, ok := alarmTypeAccumulators[alarmType]; ok {
			return existing
		}
		accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
		alarmTypeAccumulators[alarmType] = accumulator
		return accumulator
	}
	ensureChannelAccumulator := func(channelID string) *communityShortsAlarmSentHistoryDatasetComparisonAccumulator {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			return nil
		}
		if existing, ok := channelAccumulators[trimmed]; ok {
			return existing
		}
		accumulator := &communityShortsAlarmSentHistoryDatasetComparisonAccumulator{}
		channelAccumulators[trimmed] = accumulator
		return accumulator
	}

	for i := range rows {
		row := rows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.SentPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.SentPostCount++
		}
	}

	for i := range referenceRows {
		row := referenceRows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.BaselinePostCount++
			applyCommunityShortsAlarmSentHistoryDatasetVerdict(accumulator, row.VerificationVerdict)
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.BaselinePostCount++
			applyCommunityShortsAlarmSentHistoryDatasetVerdict(accumulator, row.VerificationVerdict)
		}
	}

	for i := range verificationRows {
		row := verificationRows[i]
		if row.Verdict != trackingrepo.ObservationPostComparisonVerdictUnexpectedSent {
			continue
		}
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.UnexpectedSentPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.UnexpectedSentPostCount++
		}
	}

	for i := range missingAlarmRows {
		row := missingAlarmRows[i]
		if accumulator := ensureAlarmTypeAccumulator(row.AlarmType); accumulator != nil {
			accumulator.MissingAlarmPostCount++
		}
		if accumulator := ensureChannelAccumulator(row.ChannelID); accumulator != nil {
			accumulator.MissingAlarmPostCount++
		}
	}

	results := CommunityShortsAlarmSentHistoryDatasetResults{
		MissingAlarmEvaluated:     missingAlarmEvaluated,
		MissingAlarmPostCount:     summary.MissingAlarmPostCount,
		MissingSendStatePostCount: summary.MissingSendStatePostCount,
		AttemptedMissingPostCount: summary.AttemptedMissingPostCount,
		NotSentMissingPostCount:   summary.NotSentMissingPostCount,
		MissingAlarmZero:          missingAlarmEvaluated && summary.MissingAlarmPostCount == 0,
	}

	if len(alarmTypeAccumulators) > 0 {
		keys := make([]domain.AlarmType, 0, len(alarmTypeAccumulators))
		for alarmType := range alarmTypeAccumulators {
			keys = append(keys, alarmType)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			return keys[i] < keys[j]
		})
		results.AlarmTypeComparisons = make([]CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison, 0, len(keys))
		for i := range keys {
			alarmType := keys[i]
			accumulator := alarmTypeAccumulators[alarmType]
			results.AlarmTypeComparisons = append(results.AlarmTypeComparisons, CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison{
				AlarmType:                        alarmType,
				BaselinePostCount:                accumulator.BaselinePostCount,
				SentPostCount:                    accumulator.SentPostCount,
				MatchedPostCount:                 accumulator.MatchedPostCount,
				UnsentPostCount:                  accumulator.UnsentPostCount,
				DuplicateSentPostCount:           accumulator.DuplicateSentPostCount,
				UnexpectedSentPostCount:          accumulator.UnexpectedSentPostCount,
				IdentifierMismatchCandidateCount: accumulator.IdentifierMismatchCandidateCount,
				MissingAlarmPostCount:            accumulator.MissingAlarmPostCount,
			})
		}
	}

	if len(channelAccumulators) > 0 {
		keys := make([]string, 0, len(channelAccumulators))
		for channelID := range channelAccumulators {
			keys = append(keys, channelID)
		}
		sort.Strings(keys)
		results.ChannelComparisons = make([]CommunityShortsAlarmSentHistoryDatasetChannelComparison, 0, len(keys))
		for i := range keys {
			channelID := keys[i]
			accumulator := channelAccumulators[channelID]
			results.ChannelComparisons = append(results.ChannelComparisons, CommunityShortsAlarmSentHistoryDatasetChannelComparison{
				ChannelID:                        channelID,
				BaselinePostCount:                accumulator.BaselinePostCount,
				SentPostCount:                    accumulator.SentPostCount,
				MatchedPostCount:                 accumulator.MatchedPostCount,
				UnsentPostCount:                  accumulator.UnsentPostCount,
				DuplicateSentPostCount:           accumulator.DuplicateSentPostCount,
				UnexpectedSentPostCount:          accumulator.UnexpectedSentPostCount,
				IdentifierMismatchCandidateCount: accumulator.IdentifierMismatchCandidateCount,
				MissingAlarmPostCount:            accumulator.MissingAlarmPostCount,
			})
		}
	}

	return results
}

func applyCommunityShortsAlarmSentHistoryDatasetVerdict(
	accumulator *communityShortsAlarmSentHistoryDatasetComparisonAccumulator,
	verdict trackingrepo.ObservationPostComparisonVerdict,
) {
	if accumulator == nil {
		return
	}
	switch verdict {
	case trackingrepo.ObservationPostComparisonVerdictMatched:
		accumulator.MatchedPostCount++
	case trackingrepo.ObservationPostComparisonVerdictUnsent:
		accumulator.UnsentPostCount++
	case trackingrepo.ObservationPostComparisonVerdictDuplicateSent:
		accumulator.DuplicateSentPostCount++
	case trackingrepo.ObservationPostComparisonVerdictIdentifierMismatchCandidate:
		accumulator.IdentifierMismatchCandidateCount++
	}
}
