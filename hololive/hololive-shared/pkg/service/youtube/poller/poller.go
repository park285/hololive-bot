package poller

import (
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/pollers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/resolver"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/scheduler"
)

type NotificationRouteRequest = polling.NotificationRouteRequest

type NotificationRouteDecider = polling.NotificationRouteDecider

type ViewerSampleCleanerConfig = polling.ViewerSampleCleanerConfig

type ViewerSampleCleaner = polling.ViewerSampleCleaner

type PendingPublishedAtResolverPoller = resolver.PendingPublishedAtResolverPoller

type RateLimiter = scheduler.RateLimiter

type ShortsPoller = pollers.ShortsPoller

type BudgetSource = polling.BudgetSource

type BudgetProfile = polling.BudgetProfile

type BudgetBurstClass = polling.BudgetBurstClass

type BudgetPriority = polling.BudgetPriority

type BudgetJob = polling.BudgetJob

type BudgetDecision = polling.BudgetDecision

type BudgetReservation = polling.BudgetReservation

type GlobalBudgetLimiter = polling.GlobalBudgetLimiter

type SourceCooldownReporter = polling.SourceCooldownReporter

type BudgetContext = polling.BudgetContext

type Poller = scheduler.Poller

type Job = scheduler.Job

type Priority = scheduler.Priority

type Scheduler = scheduler.Scheduler

type PollerTargetSync = scheduler.PollerTargetSync

type SchedulerConfig = scheduler.SchedulerConfig

type JobClaimResult = polling.JobClaimResult

type JobClaimStatus = polling.JobClaimStatus

type JobClaim = polling.JobClaim

type JobClaimer = polling.JobClaimer

type ChannelStatsPoller = pollers.ChannelStatsPoller

type VideosPoller = pollers.VideosPoller

type CommunityPoller = pollers.CommunityPoller

type PendingPublishedAtResolver = resolver.PendingPublishedAtResolver

type LivePoller = pollers.LivePoller

type LiveStatusProvider = pollers.LiveStatusProvider

type Metrics = polling.Metrics

const (
	PendingPublishedAtResolverPollerName          = resolver.PendingPublishedAtResolverPollerName
	PendingPublishedAtResolverCandidatePollerName = resolver.PendingPublishedAtResolverCandidatePollerName

	PriorityLow    = scheduler.PriorityLow
	PriorityNormal = scheduler.PriorityNormal
	PriorityHigh   = scheduler.PriorityHigh
	PriorityBoost  = scheduler.PriorityBoost

	BudgetSourceYouTubeScraper  = polling.BudgetSourceYouTubeScraper
	BudgetSourceHolodexLive     = polling.BudgetSourceHolodexLive
	BudgetSourceBrowserSnapshot = polling.BudgetSourceBrowserSnapshot
	BudgetSourceProxy           = polling.BudgetSourceProxy
	BudgetSourcePostgresWrite   = polling.BudgetSourcePostgresWrite

	BudgetBurstPrimary  = polling.BudgetBurstPrimary
	BudgetBurstBackfill = polling.BudgetBurstBackfill
	BudgetBurstFallback = polling.BudgetBurstFallback

	BudgetPriorityHigh   = polling.BudgetPriorityHigh
	BudgetPriorityNormal = polling.BudgetPriorityNormal
	BudgetPriorityLow    = polling.BudgetPriorityLow

	JobClaimAcquired         = polling.JobClaimAcquired
	JobClaimPeerOwned        = polling.JobClaimPeerOwned
	JobClaimAlreadyCompleted = polling.JobClaimAlreadyCompleted
	JobClaimUnavailable      = polling.JobClaimUnavailable
)

var DefaultViewerSampleCleanerConfig = polling.DefaultViewerSampleCleanerConfig

var NewViewerSampleCleaner = polling.NewViewerSampleCleaner

var NewPendingPublishedAtResolverPoller = resolver.NewPendingPublishedAtResolverPoller

var NewRateLimiter = scheduler.NewRateLimiter

var NewShortsPoller = pollers.NewShortsPoller

var DefaultSchedulerConfig = scheduler.DefaultSchedulerConfig

var NewScheduler = scheduler.NewScheduler

var NewChannelStatsPoller = pollers.NewChannelStatsPoller

var NewVideosPoller = pollers.NewVideosPoller

var NewCommunityPoller = pollers.NewCommunityPoller

var NewPendingPublishedAtResolver = resolver.NewPendingPublishedAtResolver

var NewPendingPublishedAtResolverWithControls = resolver.NewPendingPublishedAtResolverWithControls

var NewLivePoller = pollers.NewLivePoller

var NewLivePollerWithStatusProvider = pollers.NewLivePollerWithStatusProvider

var NewMetrics = polling.NewMetrics
