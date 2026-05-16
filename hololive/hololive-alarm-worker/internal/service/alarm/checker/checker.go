package checker

import checking "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"

type ChzzkChecker = checking.ChzzkChecker
type Notifier = checking.Notifier
type PersistedYouTubeLiveSession = checking.PersistedYouTubeLiveSession
type PgYouTubeLiveSessionSource = checking.PgYouTubeLiveSessionSource
type PgYouTubeLiveSessionSourceOptions = checking.PgYouTubeLiveSessionSourceOptions
type Runner = checking.Runner
type SendResult = checking.SendResult
type Sender = checking.Sender
type TwitchChecker = checking.TwitchChecker
type YouTubeChecker = checking.YouTubeChecker
type YouTubeLiveSessionSource = checking.YouTubeLiveSessionSource

var NewChzzkChecker = checking.NewChzzkChecker
var NewNotifier = checking.NewNotifier
var NewTwitchChecker = checking.NewTwitchChecker
var NewYouTubeChecker = checking.NewYouTubeChecker
var NewYouTubeCheckerWithPersistedLiveSource = checking.NewYouTubeCheckerWithPersistedLiveSource
var NewPgYouTubeLiveSessionSource = checking.NewPgYouTubeLiveSessionSource
var NewPgYouTubeLiveSessionSourceWithOptions = checking.NewPgYouTubeLiveSessionSourceWithOptions
