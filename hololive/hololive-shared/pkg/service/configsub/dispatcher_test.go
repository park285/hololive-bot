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
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewApplyFn_ScraperProxy(t *testing.T) {
	called := false
	var got contractssettings.ScraperProxyPayloadV1

	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			called = true
			got = payload
		},
	})

	payload, err := json.Marshal(contractssettings.ScraperProxyPayloadV1{Enabled: true})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	applyFn(ConfigUpdate{Type: contractssettings.UpdateTypeScraperProxy, Payload: payload})

	assert.True(t, called)
	assert.Equal(t, contractssettings.ScraperProxyPayloadV1{Enabled: true}, got)
}

func TestNewApplyFn_AlarmAdvanceMinutes(t *testing.T) {
	called := false
	var got contractssettings.AlarmAdvanceMinutesPayloadV1

	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			called = true
			got = payload
		},
	})

	payload, err := json.Marshal(contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 15})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	applyFn(ConfigUpdate{Type: contractssettings.UpdateTypeAlarmAdvanceMinutes, Payload: payload})

	assert.True(t, called)
	assert.Equal(t, contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 15}, got)
}

func TestNewApplyFn_MemberNewsWeeklyRunNow(t *testing.T) {
	called := false
	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		MemberNewsWeeklyNow: func() {
			called = true
		},
	})

	applyFn(ConfigUpdate{Type: contractssettings.UpdateTypeMemberNewsRunNow})

	assert.True(t, called)
}

func TestNewApplyFn_DecodeErrorDoesNotInvokeHandler(t *testing.T) {
	called := false
	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			called = true
		},
	})

	applyFn(ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":"not-bool"}`),
	})

	assert.False(t, called)
}

func TestNewApplyFn_Unknown(t *testing.T) {
	t.Run("custom unknown handler", func(t *testing.T) {
		called := false
		var got string
		applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
			Unknown: func(updateType string) {
				called = true
				got = updateType
			},
		})

		applyFn(ConfigUpdate{Type: "unknown"})

		assert.True(t, called)
		assert.Equal(t, "unknown", got)
	})

	t.Run("default unknown logger path", func(t *testing.T) {
		applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{})
		assert.NotPanics(t, func() {
			applyFn(ConfigUpdate{Type: "unknown"})
		})
	})
}
