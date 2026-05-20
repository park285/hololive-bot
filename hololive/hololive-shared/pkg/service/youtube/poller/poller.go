package poller

import polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling"

type NotificationRouteRequest = polling.NotificationRouteRequest

type NotificationRouteDecider = polling.NotificationRouteDecider

type ViewerSampleCleanerConfig = polling.ViewerSampleCleanerConfig

type ViewerSampleCleaner = polling.ViewerSampleCleaner

type PendingPublishedAtResolverPoller = polling.PendingPublishedAtResolverPoller

type RateLimiter = polling.RateLimiter

type ShortsPoller = polling.ShortsPoller

type Poller = polling.Poller

type Job = polling.Job

type Priority = polling.Priority

type Scheduler = polling.Scheduler

type PollerTargetSync = polling.PollerTargetSync

type SchedulerConfig = polling.SchedulerConfig

type JobClaimResult = polling.JobClaimResult

type JobClaimStatus = polling.JobClaimStatus

type JobClaim = polling.JobClaim

type JobClaimer = polling.JobClaimer

type ChannelStatsPoller = polling.ChannelStatsPoller

type VideosPoller = polling.VideosPoller

type CommunityPoller = polling.CommunityPoller

type PendingPublishedAtResolver = polling.PendingPublishedAtResolver

type LivePoller = polling.LivePoller

type LiveStatusProvider = polling.LiveStatusProvider

const (
	PendingPublishedAtResolverPollerName          = polling.PendingPublishedAtResolverPollerName
	PendingPublishedAtResolverCandidatePollerName = polling.PendingPublishedAtResolverCandidatePollerName

	PriorityLow    = polling.PriorityLow
	PriorityNormal = polling.PriorityNormal
	PriorityHigh   = polling.PriorityHigh
	PriorityBoost  = polling.PriorityBoost

	JobClaimAcquired         = polling.JobClaimAcquired
	JobClaimPeerOwned        = polling.JobClaimPeerOwned
	JobClaimAlreadyCompleted = polling.JobClaimAlreadyCompleted
	JobClaimUnavailable      = polling.JobClaimUnavailable
)

var DefaultViewerSampleCleanerConfig = polling.DefaultViewerSampleCleanerConfig

var NewViewerSampleCleaner = polling.NewViewerSampleCleaner

var NewPendingPublishedAtResolverPoller = polling.NewPendingPublishedAtResolverPoller

var NewRateLimiter = polling.NewRateLimiter

var NewShortsPoller = polling.NewShortsPoller

var DefaultSchedulerConfig = polling.DefaultSchedulerConfig

var NewScheduler = polling.NewScheduler

var NewChannelStatsPoller = polling.NewChannelStatsPoller

var NewVideosPoller = polling.NewVideosPoller

var NewCommunityPoller = polling.NewCommunityPoller

var NewPendingPublishedAtResolver = polling.NewPendingPublishedAtResolver

var NewPendingPublishedAtResolverWithControls = polling.NewPendingPublishedAtResolverWithControls

var NewLivePoller = polling.NewLivePoller

var NewLivePollerWithStatusProvider = polling.NewLivePollerWithStatusProvider
