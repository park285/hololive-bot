package server

import api "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"

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
type DomainHandlers = api.DomainHandlers
type MajorEventHandler = api.MajorEventHandler
type MemberHandler = api.MemberHandler
type MilestoneHandler = api.MilestoneHandler
type ProfileHandler = api.ProfileHandler
type RoomHandler = api.RoomHandler
type SettingsAPIHandler = api.SettingsAPIHandler
type StatsHandler = api.StatsHandler
type StreamHandler = api.StreamHandler
type TemplateHandler = api.TemplateHandler
type YouTubeCommunityShortsOpsRepository = api.YouTubeCommunityShortsOpsRepository

var NewHandler = api.NewHandler
var NewAuthHandler = api.NewAuthHandler
