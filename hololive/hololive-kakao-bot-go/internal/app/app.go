package app

import botruntime "github.com/kapu/hololive-kakao-bot-go/internal/app/botruntime"

type Container = botruntime.Container
type DBIntegrationRuntime = botruntime.DBIntegrationRuntime
type FetchProfilesRuntime = botruntime.FetchProfilesRuntime
type BotRuntime = botruntime.BotRuntime

var InitializeBotDependencies = botruntime.InitializeBotDependencies
var InitializeBotRuntime = botruntime.InitializeBotRuntime
var InitializeWarmMemberCache = botruntime.InitializeWarmMemberCache
var InitializeDBIntegrationRuntime = botruntime.InitializeDBIntegrationRuntime
var InitializeFetchProfilesRuntime = botruntime.InitializeFetchProfilesRuntime
var ProvideBotRouter = botruntime.ProvideBotRouter
var Build = botruntime.Build
var BuildDBIntegrationRuntime = botruntime.BuildDBIntegrationRuntime
var BuildFetchProfilesRuntime = botruntime.BuildFetchProfilesRuntime
var BuildRuntime = botruntime.BuildRuntime
