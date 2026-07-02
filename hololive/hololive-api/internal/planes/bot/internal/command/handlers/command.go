package handlers

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"

type Command = handlercore.Command
type Event = handlercore.Event
type Dispatcher = handlercore.Dispatcher
type MemberNewsService = handlercore.MemberNewsService
type MajorEventRepository = handlercore.MajorEventRepository
type Dependencies = handlercore.Dependencies

type NormalizeFunc = handlercore.NormalizeFunc

type CelebrationCalendarFinder = handlercore.CelebrationCalendarFinder
type CalendarImageRenderer = handlercore.CalendarImageRenderer
type LiveImageRenderer = handlercore.LiveImageRenderer
type ProfileImageRenderer = handlercore.ProfileImageRenderer
type RankImageRenderer = handlercore.RankImageRenderer
