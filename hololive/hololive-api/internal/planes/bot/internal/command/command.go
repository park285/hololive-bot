package command

import handlers "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"

type AlarmCommand = handlers.AlarmCommand
type BaseCommand = handlers.BaseCommand
type Command = handlers.Command
type Dependencies = handlers.Dependencies
type Dispatcher = handlers.Dispatcher
type Event = handlers.Event
type HelpCommand = handlers.HelpCommand
type LiveCommand = handlers.LiveCommand
type MajorEventCommand = handlers.MajorEventCommand
type MajorEventRepository = handlers.MajorEventRepository
type MemberInfoCommand = handlers.MemberInfoCommand
type MemberNewsCommand = handlers.MemberNewsCommand
type MemberNewsService = handlers.MemberNewsService
type MemberNewsSubscriptionCommand = handlers.MemberNewsSubscriptionCommand
type NormalizeFunc = handlers.NormalizeFunc
type Registry = handlers.Registry
type ScheduleCommand = handlers.ScheduleCommand
type SubscriberCommand = handlers.SubscriberCommand
type UpcomingCommand = handlers.UpcomingCommand

type CalendarCommand = handlers.CalendarCommand
type CalendarImageRenderer = handlers.CalendarImageRenderer
type CelebrationCalendarFinder = handlers.CelebrationCalendarFinder

var ErrUnknownCommand = handlers.ErrUnknownCommand

var NewCachedCelebrationCalendarFinder = handlers.NewCachedCelebrationCalendarFinder
var NewCalendarCommand = handlers.NewCalendarCommand
var FindActiveMemberOrError = handlers.FindActiveMemberOrError
var FindMemberOrError = handlers.FindMemberOrError
var NewAlarmCommand = handlers.NewAlarmCommand
var NewBaseCommand = handlers.NewBaseCommand
var NewSequentialDispatcher = handlers.NewSequentialDispatcher
var NewHelpCommand = handlers.NewHelpCommand
var NewLiveCommand = handlers.NewLiveCommand
var NewMajorEventCommand = handlers.NewMajorEventCommand
var NewMemberInfoCommand = handlers.NewMemberInfoCommand
var NewMemberNewsCommand = handlers.NewMemberNewsCommand
var NewMemberNewsSubscriptionCommand = handlers.NewMemberNewsSubscriptionCommand
var NewRegistry = handlers.NewRegistry
var NewScheduleCommand = handlers.NewScheduleCommand
var NewSubscriberCommand = handlers.NewSubscriberCommand
var NewUpcomingCommand = handlers.NewUpcomingCommand
