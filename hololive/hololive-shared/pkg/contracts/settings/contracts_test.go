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

package settings_test

import (
	jsonv2 "encoding/json/v2"
	"testing"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
)

func TestSettingsPubSubContractConstants(t *testing.T) {
	t.Parallel()

	if contractssettings.PubSubChannelV1 != "config:update" {
		t.Fatalf("PubSubChannelV1 = %q", contractssettings.PubSubChannelV1)
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

	payload, err := jsonv2.Marshal(contractssettings.ScraperProxyPayloadV1{Enabled: true})
	if err != nil {
		t.Fatalf("Marshal payload error = %v", err)
	}

	update := contractssettings.ConfigUpdateV1{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: payload,
	}

	b, err := jsonv2.Marshal(update)
	if err != nil {
		t.Fatalf("Marshal update error = %v", err)
	}

	var decoded contractssettings.ConfigUpdateV1

	if err := jsonv2.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal update error = %v", err)
	}

	if decoded.Type != contractssettings.UpdateTypeScraperProxy {
		t.Fatalf("decoded.Type = %q", decoded.Type)
	}

	var decodedPayload contractssettings.ScraperProxyPayloadV1

	if err := jsonv2.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}

	if decodedPayload.Enabled != true {
		t.Fatalf("decodedPayload.Enabled = %v", decodedPayload.Enabled)
	}
}
