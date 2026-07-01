// Package runtime은 bot plane의 안정적인 composition surface를 노출한다.
// 구현 세부는 모듈의 internal 경계 안에 보호된 채로 남는다.
package runtime

import app "github.com/kapu/hololive-api/internal/planes/bot/internal/app"

type Runtime = app.BotRuntime

var Build = app.BuildRuntime
