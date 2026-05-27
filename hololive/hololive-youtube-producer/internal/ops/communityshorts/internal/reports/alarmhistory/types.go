package alarmhistory

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendstate"
)

type DatasetCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type DatasetQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type DatasetSummary struct {
	CollectedRowCount                int        `json:"collected_row_count"`
	DuplicateRowCount                int        `json:"duplicate_row_count"`
	SentPostCount                    int        `json:"sent_post_count"`
	CommunitySentPostCount           int        `json:"community_sent_post_count"`
	ShortsSentPostCount              int        `json:"shorts_sent_post_count"`
	BaselinePostCount                int        `json:"baseline_post_count"`
	MatchedPostCount                 int        `json:"matched_post_count"`
	UnsentPostCount                  int        `json:"unsent_post_count"`
	DuplicateSentPostCount           int        `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int        `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int        `json:"identifier_mismatch_candidate_count"`
	VerificationRowCount             int        `json:"verification_row_count"`
	ReferenceRowCount                int        `json:"reference_row_count"`
	SendStatePostCount               int        `json:"send_state_post_count"`
	MissingAlarmPostCount            int        `json:"missing_alarm_post_count"`
	MissingSendStatePostCount        int        `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount        int        `json:"attempted_missing_post_count"`
	NotSentMissingPostCount          int        `json:"not_sent_missing_post_count"`
	EarliestAlarmSentAt              *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt                *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type DatasetAlarmTypeComparison struct {
	AlarmType                        domain.AlarmType `json:"alarm_type"`
	BaselinePostCount                int              `json:"baseline_post_count"`
	SentPostCount                    int              `json:"sent_post_count"`
	MatchedPostCount                 int              `json:"matched_post_count"`
	UnsentPostCount                  int              `json:"unsent_post_count"`
	DuplicateSentPostCount           int              `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int              `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int              `json:"identifier_mismatch_candidate_count"`
	MissingAlarmPostCount            int              `json:"missing_alarm_post_count"`
}

type DatasetChannelComparison struct {
	ChannelID                        string `json:"channel_id"`
	BaselinePostCount                int    `json:"baseline_post_count"`
	SentPostCount                    int    `json:"sent_post_count"`
	MatchedPostCount                 int    `json:"matched_post_count"`
	UnsentPostCount                  int    `json:"unsent_post_count"`
	DuplicateSentPostCount           int    `json:"duplicate_sent_post_count"`
	UnexpectedSentPostCount          int    `json:"unexpected_sent_post_count"`
	IdentifierMismatchCandidateCount int    `json:"identifier_mismatch_candidate_count"`
	MissingAlarmPostCount            int    `json:"missing_alarm_post_count"`
}

type DatasetResults struct {
	MissingAlarmEvaluated     bool                         `json:"missing_alarm_evaluated"`
	MissingAlarmPostCount     int                          `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int                          `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int                          `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int                          `json:"not_sent_missing_post_count"`
	MissingAlarmZero          bool                         `json:"missing_alarm_zero"`
	AlarmTypeComparisons      []DatasetAlarmTypeComparison `json:"alarm_type_comparisons,omitempty"`
	ChannelComparisons        []DatasetChannelComparison   `json:"channel_comparisons,omitempty"`
}

type DatasetRow struct {
	AlarmType         domain.AlarmType `json:"alarm_type"`
	PostKey           string           `json:"post_key,omitempty"`
	PostID            string           `json:"post_id"`
	ContentID         string           `json:"content_id"`
	ChannelID         string           `json:"channel_id"`
	ActualPublishedAt *time.Time       `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time        `json:"detected_at"`
	AlarmSentAt       time.Time        `json:"alarm_sent_at"`
}

type DatasetVerificationRow struct {
	Verdict                trackingrepo.ObservationPostComparisonVerdict                   `json:"verdict"`
	Reason                 trackingrepo.ObservationPostComparisonVerdictReason             `json:"reason"`
	AlarmType              domain.AlarmType                                                `json:"alarm_type"`
	ChannelID              string                                                          `json:"channel_id"`
	PostID                 string                                                          `json:"post_id,omitempty"`
	PostKey                string                                                          `json:"post_key,omitempty"`
	ContentID              string                                                          `json:"content_id,omitempty"`
	ActualPublishedAt      *time.Time                                                      `json:"actual_published_at,omitempty"`
	DetectedAt             *time.Time                                                      `json:"detected_at,omitempty"`
	AlarmSentAt            *time.Time                                                      `json:"alarm_sent_at,omitempty"`
	MatchPublishedAt       *time.Time                                                      `json:"match_published_at,omitempty"`
	MatchTitleHint         string                                                          `json:"match_title_hint,omitempty"`
	MatchBasis             []string                                                        `json:"match_basis,omitempty"`
	ReviewStatus           trackingrepo.ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status,omitempty"`
	BaselineCount          int                                                             `json:"baseline_count"`
	SentCount              int                                                             `json:"sent_count"`
	RelatedBaselinePostIDs []string                                                        `json:"related_baseline_post_ids,omitempty"`
	RelatedSentPostIDs     []string                                                        `json:"related_sent_post_ids,omitempty"`
}

type DatasetReferenceRow struct {
	AlarmType           domain.AlarmType                                                `json:"alarm_type"`
	ChannelID           string                                                          `json:"channel_id"`
	ChannelPostKey      string                                                          `json:"channel_post_key"`
	PostID              string                                                          `json:"post_id"`
	ActualPublishedAt   *time.Time                                                      `json:"actual_published_at,omitempty"`
	DetectedAt          *time.Time                                                      `json:"detected_at,omitempty"`
	VerificationVerdict trackingrepo.ObservationPostComparisonVerdict                   `json:"verification_verdict"`
	VerificationReason  trackingrepo.ObservationPostComparisonVerdictReason             `json:"verification_reason"`
	SentCount           int                                                             `json:"sent_count"`
	ReviewStatus        trackingrepo.ObservationIdentifierMismatchCandidateReviewStatus `json:"review_status,omitempty"`
	RelatedSentPostIDs  []string                                                        `json:"related_sent_post_ids,omitempty"`
}

type MissingAlarmReason string

const (
	MissingAlarmReasonSendStateMissing MissingAlarmReason = "send_state_missing"
	MissingAlarmReasonAttempted        MissingAlarmReason = "attempted_without_success"
	MissingAlarmReasonNotSent          MissingAlarmReason = "not_sent"
)

type DatasetMissingAlarmRow struct {
	MissingReason       MissingAlarmReason                                  `json:"missing_reason"`
	SendState           sendstate.PerPostState                              `json:"send_state,omitempty"`
	AlarmType           domain.AlarmType                                    `json:"alarm_type"`
	ChannelID           string                                              `json:"channel_id"`
	ChannelPostKey      string                                              `json:"channel_post_key"`
	PostKey             string                                              `json:"post_key,omitempty"`
	PostID              string                                              `json:"post_id"`
	ActualPublishedAt   *time.Time                                          `json:"actual_published_at,omitempty"`
	DetectedAt          *time.Time                                          `json:"detected_at,omitempty"`
	StateContentID      string                                              `json:"state_content_id,omitempty"`
	StateDetectedAt     *time.Time                                          `json:"state_detected_at,omitempty"`
	StateAlarmSentAt    *time.Time                                          `json:"state_alarm_sent_at,omitempty"`
	VerificationVerdict trackingrepo.ObservationPostComparisonVerdict       `json:"verification_verdict"`
	VerificationReason  trackingrepo.ObservationPostComparisonVerdictReason `json:"verification_reason"`
	RelatedSentPostIDs  []string                                            `json:"related_sent_post_ids,omitempty"`
}

type DatasetReport struct {
	GeneratedAt      time.Time                                    `json:"generated_at"`
	Query            DatasetQuery                                 `json:"query"`
	Summary          DatasetSummary                               `json:"summary"`
	Results          DatasetResults                               `json:"results"`
	Comparison       trackingrepo.ObservationPostComparisonResult `json:"comparison"`
	Rows             []DatasetRow                                 `json:"rows"`
	VerificationRows []DatasetVerificationRow                     `json:"verification_rows"`
	ReferenceRows    []DatasetReferenceRow                        `json:"reference_rows"`
	MissingAlarmRows []DatasetMissingAlarmRow                     `json:"missing_alarm_rows"`
}

type CommunityCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type CommunitySummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type CommunityReport struct {
	GeneratedAt time.Time                                     `json:"generated_at"`
	Query       CommunityQuery                                `json:"query"`
	Summary     CommunitySummary                              `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult  `json:"comparison"`
	Rows        []trackingrepo.ObservationAlarmSentHistoryRow `json:"rows"`
}

type ShortsCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type ShortsQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type ShortsSummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type ShortsReport struct {
	GeneratedAt time.Time                                     `json:"generated_at"`
	Query       ShortsQuery                                   `json:"query"`
	Summary     ShortsSummary                                 `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult  `json:"comparison"`
	Rows        []trackingrepo.ObservationAlarmSentHistoryRow `json:"rows"`
}
