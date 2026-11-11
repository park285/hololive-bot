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

package alarm_test

import (
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestAlarmQueueContractConstants(t *testing.T) {
	t.Parallel()

	if contractsalarm.QueueEnvelopeVersionV1 != 1 {
		t.Fatalf("QueueEnvelopeVersionV1 = %d", contractsalarm.QueueEnvelopeVersionV1)
	}
}

func TestAlarmHTTPRouteContracts(t *testing.T) {
	t.Parallel()

	if contractsalarm.BasePath != "/internal/alarm" {
		t.Fatalf("BasePath = %q, want /internal/alarm", contractsalarm.BasePath)
	}

	if contractsalarm.AddPath != "/internal/alarm/add" {
		t.Fatalf("AddPath = %q, want /internal/alarm/add", contractsalarm.AddPath)
	}

	if contractsalarm.RemovePath != "/internal/alarm/remove" {
		t.Fatalf("RemovePath = %q, want /internal/alarm/remove", contractsalarm.RemovePath)
	}

	if got := contractsalarm.RoomAlarmsViewPath("room1"); got != "/internal/alarm/room/room1/view" {
		t.Fatalf("RoomAlarmsViewPath() = %q, want /internal/alarm/room/room1/view", got)
	}

	if contractsalarm.SettingsPath != "/internal/alarm/settings" {
		t.Fatalf("SettingsPath = %q, want /internal/alarm/settings", contractsalarm.SettingsPath)
	}

	if contractsalarm.RoomNamePath != "/internal/alarm/room-name" {
		t.Fatalf("RoomNamePath = %q, want /internal/alarm/room-name", contractsalarm.RoomNamePath)
	}

	if contractsalarm.KeysPath != "/internal/alarm/keys" {
		t.Fatalf("KeysPath = %q, want /internal/alarm/keys", contractsalarm.KeysPath)
	}
}

func TestAlarmHTTPRouteContractsEscapePathParams(t *testing.T) {
	t.Parallel()

	if got := contractsalarm.RoomAlarmsViewPath("room/a b"); got != "/internal/alarm/room/room%2Fa%20b/view" {
		t.Fatalf("RoomAlarmsViewPath() = %q, want /internal/alarm/room/room%%2Fa%%20b/view", got)
	}
}
