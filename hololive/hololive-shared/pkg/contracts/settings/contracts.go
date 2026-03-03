package settings

import "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

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

// ConfigUpdateV1: Pub/Sub로 전달되는 설정 변경 메시지.
//
// 현재 hololive-shared/pkg/service/configsub.ConfigUpdate 와 동일한 형태입니다.
type ConfigUpdateV1 struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ScraperProxyPayloadV1: UpdateTypeScraperProxy 페이로드
type ScraperProxyPayloadV1 struct {
	Enabled bool `json:"enabled"`
}

// AlarmAdvanceMinutesPayloadV1: UpdateTypeAlarmAdvanceMinutes 페이로드
type AlarmAdvanceMinutesPayloadV1 struct {
	Minutes int `json:"minutes"`
}
