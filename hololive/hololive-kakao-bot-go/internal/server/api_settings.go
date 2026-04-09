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

package server

import (
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

func (h *SettingsAPIHandler) settingsHandler() *SettingsHandler {
	var publisher ConfigPublisher

	if h.valkeyCache != nil {
		publisher = configsub.NewPublisher(h.valkeyCache.GetClient())
	}

	return &SettingsHandler{
		Logger:          h.logger,
		Alarm:           h.alarm,
		Activity:        h.activity,
		ConfigPublisher: publisher,
		ReadRecentLogs: func(limit int) (any, error) {
			return h.activity.GetRecentLogs(limit)
		},
		Settings:        h.settings,
		SettingsApplier: h.settingsApplier,
	}
}

// SetRoomName: 방 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetRoomName(c *gin.Context) {
	h.settingsHandler().SetRoomName(c)
}

// SetUserName: 사용자 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetUserName(c *gin.Context) {
	h.settingsHandler().SetUserName(c)
}

// GetLogs: 활동 로그를 반환합니다.
func (h *SettingsAPIHandler) GetLogs(c *gin.Context) {
	h.settingsHandler().GetLogs(c)
}

// GetSettings: 현재 설정을 반환합니다.
func (h *SettingsAPIHandler) GetSettings(c *gin.Context) {
	h.settingsHandler().GetSettings(c)
}

// UpdateSettings: 설정을 업데이트합니다.
func (h *SettingsAPIHandler) UpdateSettings(c *gin.Context) {
	h.settingsHandler().UpdateSettings(c)
}

// UpdateLLMSettings: llm-scheduler 런타임 설정/실행 트리거를 업데이트합니다.
func (h *SettingsAPIHandler) UpdateLLMSettings(c *gin.Context) {
	h.settingsHandler().UpdateLLMSettings(c)
}
