// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package configsub

import (
	"log/slog"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
)

// ApplyHandlers: 설정 업데이트 타입별 적용 핸들러 집합.
// nil 핸들러는 해당 타입을 무시하며, Unknown이 nil이면 기본 경고 로깅을 사용한다.
type ApplyHandlers struct {
	ScraperProxy        func(contractssettings.ScraperProxyPayloadV1)
	AlarmAdvanceMinutes func(contractssettings.AlarmAdvanceMinutesPayloadV1)
	MemberNewsWeeklyNow func()
	Unknown             func(updateType string)
}

// NewApplyFn: 타입 안전 설정 업데이트 적용 함수를 생성한다.
func NewApplyFn(logger *slog.Logger, handlers ApplyHandlers) func(ConfigUpdate) {
	if logger == nil {
		logger = slog.Default()
	}

	return func(update ConfigUpdate) {
		switch update.Type {
		case contractssettings.UpdateTypeScraperProxy:
			if handlers.ScraperProxy == nil {
				logger.Debug("Ignoring config update type: handler not configured", slog.String("type", update.Type))
				return
			}

			var payload contractssettings.ScraperProxyPayloadV1
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to decode config update payload",
					slog.String("type", update.Type),
					slog.Any("error", err),
				)
				return
			}

			handlers.ScraperProxy(payload)

		case contractssettings.UpdateTypeAlarmAdvanceMinutes:
			if handlers.AlarmAdvanceMinutes == nil {
				logger.Debug("Ignoring config update type: handler not configured", slog.String("type", update.Type))
				return
			}

			var payload contractssettings.AlarmAdvanceMinutesPayloadV1
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to decode config update payload",
					slog.String("type", update.Type),
					slog.Any("error", err),
				)
				return
			}

			handlers.AlarmAdvanceMinutes(payload)

		case contractssettings.UpdateTypeMemberNewsRunNow:
			if handlers.MemberNewsWeeklyNow == nil {
				logger.Debug("Ignoring config update type: handler not configured", slog.String("type", update.Type))
				return
			}
			handlers.MemberNewsWeeklyNow()

		default:
			if handlers.Unknown != nil {
				handlers.Unknown(update.Type)
				return
			}

			logger.Warn("Unknown config update type", slog.String("type", update.Type))
		}
	}
}
