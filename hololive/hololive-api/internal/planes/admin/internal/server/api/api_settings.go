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

package api

import (
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

func (h *SettingsAPIHandler) settingsHandler() *SettingsHandler {
	if h == nil || h.Handler == nil {
		return &SettingsHandler{}
	}

	var publisher ConfigPublisher
	if h.valkeyCache != nil {
		publisher = configsub.NewPublisher(h.valkeyCache.GetClient())
	}

	var readRecentLogs SettingsReadRecentLogsFunc
	if h.activity != nil {
		readRecentLogs = func(limit int) (any, error) {
			return h.activity.GetRecentLogs(limit)
		}
	}

	return &SettingsHandler{
		Logger:          h.logger,
		Alarm:           h.alarm,
		Activity:        h.activity,
		ConfigPublisher: publisher,
		ReadRecentLogs:  readRecentLogs,
		Settings:        h.settings,
		SettingsApplier: h.settingsApplier,
	}
}

func (h *SettingsAPIHandler) SetRoomName(c *gin.Context) {
	h.settingsHandler().SetRoomName(c)
}

func (h *SettingsAPIHandler) SetUserName(c *gin.Context) {
	h.settingsHandler().SetUserName(c)
}

func (h *SettingsAPIHandler) GetLogs(c *gin.Context) {
	h.settingsHandler().GetLogs(c)
}

func (h *SettingsAPIHandler) GetSettings(c *gin.Context) {
	h.settingsHandler().GetSettings(c)
}

func (h *SettingsAPIHandler) UpdateSettings(c *gin.Context) {
	h.settingsHandler().UpdateSettings(c)
}

func (h *SettingsAPIHandler) UpdateLLMSettings(c *gin.Context) {
	h.settingsHandler().UpdateLLMSettings(c)
}
