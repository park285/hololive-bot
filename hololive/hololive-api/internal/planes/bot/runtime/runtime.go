// Package runtime exposes the stable composition surface for the bot plane.
// Implementation details remain protected by the module's internal boundary.
package runtime

import app "github.com/kapu/hololive-api/internal/planes/bot/internal/app"

type Runtime = app.BotRuntime

var Build = app.BuildRuntime
