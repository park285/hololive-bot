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

package orchcmd

import (
	"maps"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type normalizeCommandKeyTestCase struct {
	name           string
	cmdType        domain.CommandType
	params         map[string]any
	wantKey        string
	wantAction     string
	wantParamsKept bool // 기존 파라미터가 그대로 유지되어야 하는 경우
}

func TestNormalizeCommandKey(t *testing.T) {
	t.Parallel()

	tests := []normalizeCommandKeyTestCase{
		{
			name:       "alarm_list → alarm 키 + action=list",
			cmdType:    domain.CommandAlarmList,
			params:     map[string]any{},
			wantKey:    CommandKeyAlarm,
			wantAction: "list",
		},
		{
			name:       "alarm_add → alarm 키 + action=add",
			cmdType:    domain.CommandAlarmAdd,
			params:     map[string]any{"channel": "pekora"},
			wantKey:    CommandKeyAlarm,
			wantAction: "add",
		},
		{
			name:       "alarm_remove → alarm 키 + action=remove",
			cmdType:    domain.CommandAlarmRemove,
			params:     map[string]any{"channel": "aqua"},
			wantKey:    CommandKeyAlarm,
			wantAction: "remove",
		},
		{
			name:       "alarm_clear → alarm 키 + action=clear",
			cmdType:    domain.CommandAlarmClear,
			params:     map[string]any{},
			wantKey:    CommandKeyAlarm,
			wantAction: "clear",
		},
		{
			name:       "alarm_invalid → alarm 키 + action=invalid",
			cmdType:    domain.CommandAlarmInvalid,
			params:     map[string]any{},
			wantKey:    CommandKeyAlarm,
			wantAction: "invalid",
		},
		{
			name:       "alarm(action 없음) → alarm 키 + 기본 action=list",
			cmdType:    domain.CommandType("alarm"),
			params:     map[string]any{},
			wantKey:    CommandKeyAlarm,
			wantAction: "list",
		},
		{
			name:           "alarm(action 있음) → alarm 키 + 기존 action 유지",
			cmdType:        domain.CommandType("alarm"),
			params:         map[string]any{"action": "custom"},
			wantKey:        CommandKeyAlarm,
			wantAction:     "custom",
			wantParamsKept: true,
		},
		{
			name:       "news_subscription(action 없음) → news_subscription 키 + 기본 action=status",
			cmdType:    domain.CommandMemberNewsSubscription,
			params:     map[string]any{},
			wantKey:    CommandKeyNewsSubscription,
			wantAction: "status",
		},
		{
			name:       "news_subscription_toggle → news_subscription 키 + action=toggle",
			cmdType:    domain.CommandType("news_subscription_toggle"),
			params:     map[string]any{},
			wantKey:    CommandKeyNewsSubscription,
			wantAction: "toggle",
		},
		{
			name:    "live → 변환 없이 live 키 반환",
			cmdType: domain.CommandLive,
			params:  map[string]any{"foo": "bar"},
			wantKey: "live",
		},
		{
			name:    "help → 변환 없이 help 키 반환",
			cmdType: domain.CommandHelp,
			params:  map[string]any{},
			wantKey: "help",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNormalizeCommandKey(t, &tc)
		})
	}
}

func assertNormalizeCommandKey(t *testing.T, tc *normalizeCommandKeyTestCase) {
	t.Helper()

	origParams := make(map[string]any, len(tc.params))
	maps.Copy(origParams, tc.params)

	gotKey, gotParams := NormalizeCommandKey(tc.cmdType, tc.params)
	if gotKey != tc.wantKey {
		t.Errorf("NormalizeCommandKey(%q) key = %q, want %q", tc.cmdType, gotKey, tc.wantKey)
	}
	assertNormalizeCommandAction(t, gotParams, tc.wantAction)
	assertNormalizeCommandKeptParams(t, gotParams, origParams, tc.wantParamsKept)
	assertOriginalParamsUnchanged(t, tc.params, origParams)
}

func assertNormalizeCommandAction(t *testing.T, gotParams map[string]any, wantAction string) {
	t.Helper()

	if wantAction == "" {
		return
	}
	gotAction, ok := gotParams["action"]
	if !ok {
		t.Fatal("gotParams에 action 키가 없음")
	}
	if gotAction != wantAction {
		t.Errorf("gotParams[action] = %q, want %q", gotAction, wantAction)
	}
}

func assertNormalizeCommandKeptParams(t *testing.T, gotParams, origParams map[string]any, wantParamsKept bool) {
	t.Helper()

	if !wantParamsKept {
		return
	}
	for k, origV := range origParams {
		if gotV := gotParams[k]; gotV != origV {
			t.Errorf("gotParams[%q] = %v, want %v (원본 파라미터 변경됨)", k, gotV, origV)
		}
	}
}

func assertOriginalParamsUnchanged(t *testing.T, params, origParams map[string]any) {
	t.Helper()

	for k, origV := range origParams {
		if params[k] != origV {
			t.Errorf("원본 params[%q]가 변경됨: got %v, want %v", k, params[k], origV)
		}
	}
}

type cloneParamsWithActionTestCase struct {
	name       string
	params     map[string]any
	action     string
	wantAction string
	wantKeys   []string
}

func TestCloneParamsWithAction(t *testing.T) {
	t.Parallel()

	tests := []cloneParamsWithActionTestCase{
		{
			name:       "nil 입력 → action만 있는 새 맵 반환",
			params:     nil,
			action:     "list",
			wantAction: "list",
			wantKeys:   []string{"action"},
		},
		{
			name:       "기존 파라미터 → action 추가 후 복제 반환",
			params:     map[string]any{"channel": "noel", "org": "hololive"},
			action:     "add",
			wantAction: "add",
			wantKeys:   []string{"channel", "org", "action"},
		},
		{
			name:       "빈 맵 → action만 있는 새 맵 반환",
			params:     map[string]any{},
			action:     "remove",
			wantAction: "remove",
			wantKeys:   []string{"action"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertCloneParamsWithAction(t, &tc)
		})
	}
}

func assertCloneParamsWithAction(t *testing.T, tc *cloneParamsWithActionTestCase) {
	t.Helper()

	origLen := len(tc.params)
	got := cloneParamsWithAction(tc.params, tc.action)
	if got["action"] != tc.wantAction {
		t.Errorf("got[action] = %v, want %q", got["action"], tc.wantAction)
	}
	for _, key := range tc.wantKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("반환된 맵에 키 %q가 없음", key)
		}
	}
	if tc.params != nil && len(tc.params) != origLen {
		t.Errorf("원본 params 길이가 변경됨: got %d, want %d", len(tc.params), origLen)
	}
	if len(tc.params) > 0 {
		got["__test_isolation__"] = true
		if _, leaked := tc.params["__test_isolation__"]; leaked {
			t.Error("cloneParamsWithAction이 원본 맵을 반환함 (복제 아님)")
		}
	}
}
