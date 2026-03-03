package settings_test

import (
	"testing"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

func TestSettingsPubSubContractConstants(t *testing.T) {
	t.Parallel()

	if contractssettings.PubSubChannelV1 != configsub.DefaultChannel {
		t.Fatalf("PubSubChannelV1 = %q, DefaultChannel = %q", contractssettings.PubSubChannelV1, configsub.DefaultChannel)
	}
	if contractssettings.ConfigUpdateVersionV1 != 1 {
		t.Fatalf("ConfigUpdateVersionV1 = %d", contractssettings.ConfigUpdateVersionV1)
	}

	if contractssettings.UpdateTypeScraperProxy != "scraper_proxy" {
		t.Fatalf("UpdateTypeScraperProxy = %q", contractssettings.UpdateTypeScraperProxy)
	}
	if contractssettings.UpdateTypeAlarmAdvanceMinutes != "alarm_advance_minutes" {
		t.Fatalf("UpdateTypeAlarmAdvanceMinutes = %q", contractssettings.UpdateTypeAlarmAdvanceMinutes)
	}
	if contractssettings.UpdateTypeMemberNewsRunNow != "membernews_weekly_run_now" {
		t.Fatalf("UpdateTypeMemberNewsRunNow = %q", contractssettings.UpdateTypeMemberNewsRunNow)
	}
}

func TestConfigUpdateV1_JSONContract(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(contractssettings.ScraperProxyPayloadV1{Enabled: true})
	if err != nil {
		t.Fatalf("Marshal payload error = %v", err)
	}

	update := contractssettings.ConfigUpdateV1{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: payload,
	}

	b, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("Marshal update error = %v", err)
	}

	var decoded contractssettings.ConfigUpdateV1
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal update error = %v", err)
	}
	if decoded.Type != contractssettings.UpdateTypeScraperProxy {
		t.Fatalf("decoded.Type = %q", decoded.Type)
	}

	var decodedPayload contractssettings.ScraperProxyPayloadV1
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if decodedPayload.Enabled != true {
		t.Fatalf("decodedPayload.Enabled = %v", decodedPayload.Enabled)
	}
}
