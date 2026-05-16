package server

import api "github.com/kapu/hololive-admin-api/internal/server/internal/api"

type APIHandler = api.APIHandler
type AlarmAPIHandler = api.AlarmAPIHandler
type AuthHandler = api.AuthHandler
type ConfigPublisher = api.ConfigPublisher
type DataEntry = api.DataEntry
type DomainAPIHandlers = api.DomainAPIHandlers
type MajorEventAPIHandler = api.MajorEventAPIHandler
type MajorEventMonthlyScheduler = api.MajorEventMonthlyScheduler
type MajorEventScheduler = api.MajorEventScheduler
type MemberAPIHandler = api.MemberAPIHandler
type MilestoneAPIHandler = api.MilestoneAPIHandler
type OAuthAPIHandler = api.OAuthAPIHandler
type ProfileAPIHandler = api.ProfileAPIHandler
type ProfileData = api.ProfileData
type ProfileResponse = api.ProfileResponse
type RoomAPIHandler = api.RoomAPIHandler
type SettingsAPIHandler = api.SettingsAPIHandler
type SettingsActivityLogger = api.SettingsActivityLogger
type SettingsHandler = api.SettingsHandler
type SettingsReadRecentLogsFunc = api.SettingsReadRecentLogsFunc
type SocialLink = api.SocialLink
type StatsAPIHandler = api.StatsAPIHandler
type StreamAPIHandler = api.StreamAPIHandler
type TemplateAPIHandler = api.TemplateAPIHandler
type TranslatedData = api.TranslatedData
type YouTubeCommunityShortsOpsChannel = api.YouTubeCommunityShortsOpsChannel
type YouTubeCommunityShortsOpsOverview = api.YouTubeCommunityShortsOpsOverview
type YouTubeCommunityShortsOpsRepository = api.YouTubeCommunityShortsOpsRepository
type YouTubeCommunityShortsOpsResponse = api.YouTubeCommunityShortsOpsResponse

var NewAPIHandler = api.NewAPIHandler
var NewAuthHandler = api.NewAuthHandler
