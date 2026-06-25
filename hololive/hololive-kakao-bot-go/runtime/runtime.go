// Package runtime exposes the stable composition surface for the bot plane.
// Implementation details remain protected by the module's internal boundary.
package runtime

import app "github.com/kapu/hololive-kakao-bot-go/internal/app"

type Runtime = app.BotRuntime

var Build = app.BuildRuntime
