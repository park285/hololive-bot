package server

import httpserver "github.com/kapu/hololive-shared/pkg/server/internal/httpserver"

type BaseMiddlewareOptions = httpserver.BaseMiddlewareOptions
type RuntimeRouterOptions = httpserver.RuntimeRouterOptions
type StreamState = httpserver.StreamState
type StreamMemberRepository = httpserver.StreamMemberRepository
type StreamRespondErrorFunc = httpserver.StreamRespondErrorFunc
type StreamRespondInternalErrorFunc = httpserver.StreamRespondInternalErrorFunc
type StreamHandler = httpserver.StreamHandler
type ChannelResponse = httpserver.ChannelResponse
type MajorEventScheduler = httpserver.MajorEventScheduler
type MajorEventMonthlyScheduler = httpserver.MajorEventMonthlyScheduler
type MemberNewsWeeklyScheduler = httpserver.MemberNewsWeeklyScheduler
type TriggerHandler = httpserver.TriggerHandler
type RuntimeHTTPServers = httpserver.RuntimeHTTPServers

const (
	AppScheme                         = httpserver.AppScheme
	CallbackPath                      = httpserver.CallbackPath
	ChannelStatsCacheKey              = httpserver.ChannelStatsCacheKey
	ChannelStatsCacheTTL              = httpserver.ChannelStatsCacheTTL
	ChannelStatsRefreshLockKey        = httpserver.ChannelStatsRefreshLockKey
	ChannelStatsRefreshLockValue      = httpserver.ChannelStatsRefreshLockValue
	ChannelStatsRefreshLockTTL        = httpserver.ChannelStatsRefreshLockTTL
	MemberIndexCacheTTL               = httpserver.MemberIndexCacheTTL
	DefaultChannelStatsCacheWorkers   = httpserver.DefaultChannelStatsCacheWorkers
	DefaultChannelStatsRefreshWorkers = httpserver.DefaultChannelStatsRefreshWorkers
)

var WSUpgrader = httpserver.WSUpgrader

var SplitChannelIDs = httpserver.SplitChannelIDs
var EnableH2C = httpserver.EnableH2C
var BuildOAuthDeepLinkURL = httpserver.BuildOAuthDeepLinkURL
var BuildOAuthRedirectHTML = httpserver.BuildOAuthRedirectHTML
var RespondError = httpserver.RespondError
var RespondInternalError = httpserver.RespondInternalError
var ApplyBaseMiddleware = httpserver.ApplyBaseMiddleware
var RegisterHealthRoutes = httpserver.RegisterHealthRoutes
var NewHealthOnlyRuntimeRouter = httpserver.NewHealthOnlyRuntimeRouter
var NewTriggerRuntimeRouter = httpserver.NewTriggerRuntimeRouter
var NewH2CServer = httpserver.NewH2CServer
var NewH3Server = httpserver.NewH3Server
var NewMetricsServer = httpserver.NewMetricsServer
var NewPprofServer = httpserver.NewPprofServer
var NewRuntimeHTTPServers = httpserver.NewRuntimeHTTPServers
var StartH3Server = httpserver.StartH3Server
var ShutdownH3Server = httpserver.ShutdownH3Server
var NewRuntimeRouter = httpserver.NewRuntimeRouter
var NewStreamState = httpserver.NewStreamState
var MemberToChannelResponse = httpserver.MemberToChannelResponse
var BuildActiveMemberIndex = httpserver.BuildActiveMemberIndex
var NewTriggerHandler = httpserver.NewTriggerHandler
var InitWSUpgrader = httpserver.InitWSUpgrader
