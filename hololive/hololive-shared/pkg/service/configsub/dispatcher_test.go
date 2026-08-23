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

	jsonv2 "encoding/json/v2"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
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

	payload, err := jsonv2.Marshal(contractssettings.ScraperProxyPayloadV1{Enabled: true})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	applyFn(contractssettings.ConfigUpdateV1{Type: contractssettings.UpdateTypeScraperProxy, Payload: payload})

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

	payload, err := jsonv2.Marshal(contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 15})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	applyFn(contractssettings.ConfigUpdateV1{Type: contractssettings.UpdateTypeAlarmAdvanceMinutes, Payload: payload})

	assert.True(t, called)
	assert.Equal(t, contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 15}, got)
}

func TestNewApplyFn_DecodeErrorDoesNotInvokeHandler(t *testing.T) {
	called := false
	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			called = true
		},
	})

	applyFn(contractssettings.ConfigUpdateV1{
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

		applyFn(contractssettings.ConfigUpdateV1{Type: "unknown"})

		assert.True(t, called)
		assert.Equal(t, "unknown", got)
	})

	t.Run("default unknown logger path", func(t *testing.T) {
		applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{})
		assert.NotPanics(t, func() {
			applyFn(contractssettings.ConfigUpdateV1{Type: "unknown"})
		})
	})
}

func TestNewApplyFn_ACL(t *testing.T) {
	called := false
	var got contractssettings.ACLPayloadV1

	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		ACL: func(payload contractssettings.ACLPayloadV1) {
			called = true
			got = payload
		},
	})

	payload, err := jsonv2.Marshal(contractssettings.ACLPayloadV1{Reason: "room_add", Room: "room-1", Mode: "whitelist"})
	assert.NoError(t, err)

	applyFn(contractssettings.ConfigUpdateV1{Type: contractssettings.UpdateTypeACL, Payload: payload})

	assert.True(t, called, "acl update must reach the ACL handler")
	assert.Equal(t, "room_add", got.Reason)
	assert.Equal(t, "room-1", got.Room)
	assert.Equal(t, "whitelist", got.Mode)
}

func TestNewApplyFn_ACLWithoutHandlerDoesNotFallThroughToUnknown(t *testing.T) {
	unknownCalled := false

	applyFn := NewApplyFn(newDiscardLogger(), ApplyHandlers{
		Unknown: func(string) { unknownCalled = true },
	})

	payload, err := jsonv2.Marshal(contractssettings.ACLPayloadV1{Reason: "room_add"})
	assert.NoError(t, err)

	applyFn(contractssettings.ConfigUpdateV1{Type: contractssettings.UpdateTypeACL, Payload: payload})

	assert.False(t, unknownCalled, "a known type with no handler must not be reported as unknown")
}
