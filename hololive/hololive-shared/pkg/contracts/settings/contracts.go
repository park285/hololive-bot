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

import (
	"encoding/json/jsontext"
)

const (
	// PubSubChannelV1: 설정 변경 Pub/Sub 채널 이름 (SSOT).
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
	UpdateTypeACL                 = "acl"
)

type ConfigUpdateV1 struct {
	Type    string         `json:"type"`
	Payload jsontext.Value `json:"payload"`
}

type ScraperProxyPayloadV1 struct {
	Enabled bool `json:"enabled"`
}

type AlarmAdvanceMinutesPayloadV1 struct {
	Minutes int `json:"minutes"`
}

// 수신자는 payload가 아니라 DB에서 ACL 전체를 다시 읽는다 — 이 필드들은 진단용이며,
// 그래야 메시지 유실·순서 뒤바뀜이 있어도 최종 상태가 DB와 어긋나지 않는다.
type ACLPayloadV1 struct {
	Reason string `json:"reason,omitempty"`
	Room   string `json:"room,omitempty"`
	Mode   string `json:"mode,omitempty"`
}
