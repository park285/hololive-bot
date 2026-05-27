package communityshortsops

import (
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/channelsummary"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/continuousobservation"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/deliverylogs"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/routereport"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendstate"
)

type CommunityShortsAlarmSentHistoryDatasetCollectOptions = alarmhistory.DatasetCollectOptions
type CommunityShortsAlarmSentHistoryDatasetQuery = alarmhistory.DatasetQuery
type CommunityShortsAlarmSentHistoryDatasetSummary = alarmhistory.DatasetSummary
type CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison = alarmhistory.DatasetAlarmTypeComparison
type CommunityShortsAlarmSentHistoryDatasetChannelComparison = alarmhistory.DatasetChannelComparison
type CommunityShortsAlarmSentHistoryDatasetResults = alarmhistory.DatasetResults
type CommunityShortsAlarmSentHistoryDatasetRow = alarmhistory.DatasetRow
type CommunityShortsAlarmSentHistoryDatasetVerificationRow = alarmhistory.DatasetVerificationRow
type CommunityShortsAlarmSentHistoryDatasetReferenceRow = alarmhistory.DatasetReferenceRow
type CommunityShortsMissingAlarmReason = alarmhistory.MissingAlarmReason
type CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow = alarmhistory.DatasetMissingAlarmRow
type CommunityShortsAlarmSentHistoryDatasetReport = alarmhistory.DatasetReport
type CommunityShortsChannelSummaryReport = channelsummary.Report
type CommunityShortsChannelSummaryTotals = channelsummary.Totals
type CommunityShortsContinuousObservationStatus = continuousobservation.Status
type CommunityShortsContinuousObservationCollectOptions = continuousobservation.CollectOptions
type CommunityShortsContinuousObservationCloseoutStatus = continuousobservation.CloseoutStatus
type CommunityShortsContinuousObservationWindow = continuousobservation.Window
type CommunityShortsContinuousObservation24HCloseout = continuousobservation.Closeout24H
type CommunityShortsContinuousObservationMissingAlarmCloseout = continuousobservation.MissingAlarmCloseout
type CommunityShortsContinuousObservationStateConsistencyCloseout = continuousobservation.StateConsistencyCloseout
type CommunityShortsContinuousObservationReport = continuousobservation.Report
type CommunityShortsDeliveryLogQueryMode = deliverylogs.QueryMode
type CommunityShortsDeliveryLogCollectOptions = deliverylogs.CollectOptions
type CommunityShortsDeliveryLogReport = deliverylogs.Report
type CommunityShortsDeliveryLogQuery = deliverylogs.Query
type CommunityShortsDeliveryLogSummary = deliverylogs.Summary
type CommunityShortsDeliveryLogRow = deliverylogs.Row
type CommunityShortsLatencyCauseQueryMode = latencycause.QueryMode
type CommunityShortsInternalCauseJudgment = latencycause.InternalCauseJudgment
type CommunityShortsLatencyCauseCollectOptions = latencycause.CollectOptions
type CommunityShortsLatencyCauseQuery = latencycause.Query
type CommunityShortsLatencyCauseVerification = latencycause.Verification
type CommunityShortsLatencyCauseEvidence = latencycause.Evidence
type CommunityShortsLatencyCauseReport = latencycause.Report
type CommunityShortsLatencyCausePeriodView = latencycause.PeriodView
type CommunityShortsLatencyCauseSummary = latencycause.Summary
type CommunityShortsLatencyCauseRow = latencycause.Row
type CommunityShortsLatencyPeriodSpec = latencycause.PeriodSpec
type CommunityShortsLatencyPeriodReport = latencycause.PeriodReport
type CommunityShortsRouteVerificationReport = routereport.Report
type CommunityShortsRouteVerificationRuntime = routereport.Runtime
type CommunityShortsRouteVerificationSummary = routereport.Summary
type CommunityShortsRouteVerificationChannel = routereport.Channel
type CommunityShortsRouteVerificationRoute = routereport.Route
type CommunityShortsSendCountQueryMode = sendcounts.QueryMode
type CommunityShortsSendCountCollectOptions = sendcounts.CollectOptions
type CommunityShortsSendCountQuery = sendcounts.Query
type CommunityShortsSendCountReport = sendcounts.Report
type CommunityShortsSendCountSummary = sendcounts.Summary
type CommunityShortsSendCountVerificationStatus = sendcounts.VerificationStatus
type CommunityShortsSendCountVerification = sendcounts.Verification
type CommunityShortsSendCountRow = sendcounts.Row
type CommunityShortsPerPostSendState = sendstate.PerPostState
type CommunityShortsSendStateCollectOptions = sendstate.CollectOptions
type CommunityShortsSendStateQuery = sendstate.Query
type CommunityShortsSendStateSummary = sendstate.Summary
type CommunityShortsSendStateRow = sendstate.Row
type CommunityShortsSendStateReport = sendstate.Report
type CommunityAlarmSentHistoryCollectOptions = alarmhistory.CommunityCollectOptions
type CommunityAlarmSentHistoryQuery = alarmhistory.CommunityQuery
type CommunityAlarmSentHistorySummary = alarmhistory.CommunitySummary
type CommunityAlarmSentHistoryReport = alarmhistory.CommunityReport
type ShortsAlarmSentHistoryCollectOptions = alarmhistory.ShortsCollectOptions
type ShortsAlarmSentHistoryQuery = alarmhistory.ShortsQuery
type ShortsAlarmSentHistorySummary = alarmhistory.ShortsSummary
type ShortsAlarmSentHistoryReport = alarmhistory.ShortsReport

const (
	CommunityShortsMissingAlarmReasonSendStateMissing                      = alarmhistory.MissingAlarmReasonSendStateMissing
	CommunityShortsMissingAlarmReasonAttempted                             = alarmhistory.MissingAlarmReasonAttempted
	CommunityShortsMissingAlarmReasonNotSent                               = alarmhistory.MissingAlarmReasonNotSent
	CommunityShortsContinuousObservationStatusActive                       = continuousobservation.StatusActive
	CommunityShortsContinuousObservationStatusFinalized                    = continuousobservation.StatusFinalized
	CommunityShortsContinuousObservationCloseoutStatusPending              = continuousobservation.CloseoutStatusPending
	CommunityShortsContinuousObservationCloseoutStatusPass                 = continuousobservation.CloseoutStatusPass
	CommunityShortsContinuousObservationCloseoutStatusFail                 = continuousobservation.CloseoutStatusFail
	CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence = continuousobservation.CloseoutStatusInsufficientEvidence
	CommunityShortsInternalCauseJudgmentInternalSystem                     = latencycause.InternalCauseJudgmentInternalSystem
	CommunityShortsInternalCauseJudgmentNonInternal                        = latencycause.InternalCauseJudgmentNonInternal
	CommunityShortsPerPostSendStateSent                                    = sendstate.PerPostStateSent
	CommunityShortsPerPostSendStateAttemptedWithoutSuccess                 = sendstate.PerPostStateAttemptedWithoutSuccess
	CommunityShortsPerPostSendStateNotSent                                 = sendstate.PerPostStateNotSent
)

var CollectCommunityAlarmSentHistoryReport = alarmhistory.CollectCommunity
var BuildCommunityAlarmSentHistoryReport = alarmhistory.BuildCommunity
var RenderCommunityAlarmSentHistoryMarkdown = alarmhistory.RenderCommunityMarkdown
var CollectCommunityShortsAlarmSentHistoryDatasetReport = alarmhistory.CollectDataset
var BuildCommunityShortsAlarmSentHistoryDatasetReport = alarmhistory.BuildDataset
var RenderCommunityShortsAlarmSentHistoryDatasetMarkdown = alarmhistory.RenderDatasetMarkdown
var CollectCommunityShortsChannelSummaryReport = channelsummary.Collect
var BuildCommunityShortsChannelSummaryReport = channelsummary.Build
var RenderCommunityShortsChannelSummaryMarkdown = channelsummary.RenderMarkdown
var RenderCommunityShortsContinuousObservationMarkdown = continuousobservation.RenderMarkdown
var DefaultCommunityShortsContinuousObservationPeriodSpecs = continuousobservation.DefaultPeriodSpecs
var CollectCommunityShortsContinuousObservationReport = continuousobservation.Collect
var BuildCommunityShortsDeliveryLogReport = deliverylogs.Build
var RenderCommunityShortsDeliveryLogMarkdown = deliverylogs.RenderMarkdown
var CollectCommunityShortsDeliveryLogReport = deliverylogs.Collect
var BuildCommunityShortsLatencyCauseReport = latencycause.Build
var BuildCommunityShortsLatencyCauseReportWithQuery = latencycause.BuildWithQuery
var RenderCommunityShortsLatencyCauseMarkdown = latencycause.RenderMarkdown
var CollectCommunityShortsLatencyCauseReport = latencycause.Collect
var CollectCommunityShortsLatencyCauseReportWithOptions = latencycause.CollectWithOptions
var DefaultCommunityShortsLatencyPeriodSpecs = latencycause.DefaultPeriodSpecs
var CollectCommunityShortsLatencyPeriodReport = latencycause.CollectPeriodReport
var BuildCommunityShortsLatencyPeriodReport = latencycause.BuildPeriodReport
var RenderCommunityShortsLatencyPeriodMarkdown = latencycause.RenderPeriodMarkdown
var CollectCommunityShortsRouteVerificationReport = routereport.Collect
var BuildCommunityShortsRouteVerificationReport = routereport.Build
var RenderCommunityShortsRouteVerificationMarkdown = routereport.RenderMarkdown
var BuildCommunityShortsSendCountReport = sendcounts.Build
var BuildCommunityShortsSendCountReportWithQuery = sendcounts.BuildWithQuery
var RenderCommunityShortsSendCountMarkdown = sendcounts.RenderMarkdown
var CollectCommunityShortsSendCountReport = sendcounts.Collect
var CollectCommunityShortsSendCountReportWithOptions = sendcounts.CollectWithOptions
var BuildCommunityShortsSendStateReport = sendstate.Build
var RenderCommunityShortsSendStateMarkdown = sendstate.RenderMarkdown
var CollectCommunityShortsSendStateReport = sendstate.Collect
var CollectShortsAlarmSentHistoryReport = alarmhistory.CollectShorts
var BuildShortsAlarmSentHistoryReport = alarmhistory.BuildShorts
var RenderShortsAlarmSentHistoryMarkdown = alarmhistory.RenderShortsMarkdown
