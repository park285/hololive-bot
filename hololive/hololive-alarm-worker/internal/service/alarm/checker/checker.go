package checker

import (
	checking "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/chzzk"
	checknotifier "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/twitch"
)

type ChzzkChecker = chzzk.ChzzkChecker
type Notifier = checknotifier.Notifier
type PersistedYouTubeLiveSession = checking.PersistedYouTubeLiveSession
type PgYouTubeLiveSessionSource = checking.PgYouTubeLiveSessionSource
type PgYouTubeLiveSessionSourceOptions = checking.PgYouTubeLiveSessionSourceOptions
type Runner = checking.Runner
type SendResult = checking.SendResult
type Sender = checking.Sender
type TwitchChecker = twitch.TwitchChecker
type YouTubeChecker = checking.YouTubeChecker
type YouTubeLiveSessionSource = checking.YouTubeLiveSessionSource

var NewChzzkChecker = chzzk.NewChzzkChecker
var NewNotifier = checknotifier.NewNotifier
var NewTwitchChecker = twitch.NewTwitchChecker
var NewYouTubeChecker = checking.NewYouTubeChecker
var NewYouTubeCheckerWithPersistedLiveSource = checking.NewYouTubeCheckerWithPersistedLiveSource
var NewPgYouTubeLiveSessionSource = checking.NewPgYouTubeLiveSessionSource
var NewPgYouTubeLiveSessionSourceWithOptions = checking.NewPgYouTubeLiveSessionSourceWithOptions
