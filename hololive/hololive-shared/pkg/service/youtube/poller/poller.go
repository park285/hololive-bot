package poller

import (
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling/pollers"
)

type NotificationRouteRequest = polling.NotificationRouteRequest

type NotificationRouteDecider = polling.NotificationRouteDecider

type ViewerSampleCleanerConfig = polling.ViewerSampleCleanerConfig

type ViewerSampleCleaner = polling.ViewerSampleCleaner

type PendingPublishedAtResolverPoller = polling.PendingPublishedAtResolverPoller

type RateLimiter = polling.RateLimiter

type ShortsPoller = pollers.ShortsPoller

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

type ChannelStatsPoller = pollers.ChannelStatsPoller

type VideosPoller = pollers.VideosPoller

type CommunityPoller = pollers.CommunityPoller

type PendingPublishedAtResolver = polling.PendingPublishedAtResolver

type LivePoller = pollers.LivePoller

type LiveStatusProvider = pollers.LiveStatusProvider

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

var NewShortsPoller = pollers.NewShortsPoller

var DefaultSchedulerConfig = polling.DefaultSchedulerConfig

var NewScheduler = polling.NewScheduler

var NewChannelStatsPoller = pollers.NewChannelStatsPoller

var NewVideosPoller = pollers.NewVideosPoller

var NewCommunityPoller = pollers.NewCommunityPoller

var NewPendingPublishedAtResolver = polling.NewPendingPublishedAtResolver

var NewPendingPublishedAtResolverWithControls = polling.NewPendingPublishedAtResolverWithControls

var NewLivePoller = pollers.NewLivePoller

var NewLivePollerWithStatusProvider = pollers.NewLivePollerWithStatusProvider
