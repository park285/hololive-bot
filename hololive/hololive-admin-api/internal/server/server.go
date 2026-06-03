package server

import api "github.com/kapu/hololive-admin-api/internal/server/internal/api"

type Handler = api.Handler
type HandlerDeps = api.HandlerDeps
type CommonDeps = api.CommonDeps
type MemberDeps = api.MemberDeps
type StreamDeps = api.StreamDeps
type StatsDeps = api.StatsDeps
type SettingsDeps = api.SettingsDeps
type TemplateDeps = api.TemplateDeps
type MajorEventDeps = api.MajorEventDeps
type YouTubeOpsDeps = api.YouTubeOpsDeps
type AlarmHandler = api.AlarmHandler
type AuthHandler = api.AuthHandler
type ConfigPublisher = api.ConfigPublisher
type DataEntry = api.DataEntry
type DomainHandlers = api.DomainHandlers
type MajorEventHandler = api.MajorEventHandler
type MajorEventMonthlyScheduler = api.MajorEventMonthlyScheduler
type MajorEventScheduler = api.MajorEventScheduler
type MemberHandler = api.MemberHandler
type MilestoneHandler = api.MilestoneHandler
type OAuthHandler = api.OAuthHandler
type ProfileHandler = api.ProfileHandler
type ProfileData = api.ProfileData
type ProfileResponse = api.ProfileResponse
type RoomHandler = api.RoomHandler
type SettingsAPIHandler = api.SettingsAPIHandler
type SettingsActivityLogger = api.SettingsActivityLogger
type SettingsHandler = api.SettingsHandler
type SettingsReadRecentLogsFunc = api.SettingsReadRecentLogsFunc
type SocialLink = api.SocialLink
type StatsHandler = api.StatsHandler
type StreamHandler = api.StreamHandler
type TemplateHandler = api.TemplateHandler
type TranslatedData = api.TranslatedData
type YouTubeCommunityShortsOpsChannel = api.YouTubeCommunityShortsOpsChannel
type YouTubeCommunityShortsOpsOverview = api.YouTubeCommunityShortsOpsOverview
type YouTubeCommunityShortsOpsRepository = api.YouTubeCommunityShortsOpsRepository
type YouTubeCommunityShortsOpsResponse = api.YouTubeCommunityShortsOpsResponse

var NewHandler = api.NewHandler
var NewAuthHandler = api.NewAuthHandler
