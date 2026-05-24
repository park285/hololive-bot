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

package settings

import "github.com/park285/shared-go/pkg/json"

const (
	// PubSubChannelV1: 설정 변경 Pub/Sub 채널 이름 (SSOT).
	//
	// 현재 hololive-shared/pkg/service/configsub.DefaultChannel 과 동일합니다.
	PubSubChannelV1 = "config:update"
)

const (
	// ConfigUpdateVersionV1: 설정 업데이트 메시지 버전 (payload 내에 version 필드가 포함되지는 않음)
	ConfigUpdateVersionV1 uint8 = 1
)

const (
	UpdateTypeScraperProxy        = "scraper_proxy"
	UpdateTypeAlarmAdvanceMinutes = "alarm_advance_minutes"
	UpdateTypeMemberNewsRunNow    = "membernews_weekly_run_now"
)

// 현재 hololive-shared/pkg/service/configsub.ConfigUpdate 와 동일한 형태입니다.
type ConfigUpdateV1 struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type ScraperProxyPayloadV1 struct {
	Enabled bool `json:"enabled"`
}

type AlarmAdvanceMinutesPayloadV1 struct {
	Minutes int `json:"minutes"`
}
