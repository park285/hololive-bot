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

package bot

import (
	"maps"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNormalizeCommandKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cmdType        domain.CommandType
		params         map[string]any
		wantKey        string
		wantAction     string
		wantParamsKept bool // 기존 파라미터가 그대로 유지되어야 하는 경우
	}{
		{
			name:       "alarm_list → alarm 키 + action=list",
			cmdType:    domain.CommandAlarmList,
			params:     map[string]any{},
			wantKey:    commandKeyAlarm,
			wantAction: "list",
		},
		{
			name:       "alarm_add → alarm 키 + action=add",
			cmdType:    domain.CommandAlarmAdd,
			params:     map[string]any{"channel": "pekora"},
			wantKey:    commandKeyAlarm,
			wantAction: "add",
		},
		{
			name:       "alarm_remove → alarm 키 + action=remove",
			cmdType:    domain.CommandAlarmRemove,
			params:     map[string]any{"channel": "aqua"},
			wantKey:    commandKeyAlarm,
			wantAction: "remove",
		},
		{
			name:       "alarm_clear → alarm 키 + action=clear",
			cmdType:    domain.CommandAlarmClear,
			params:     map[string]any{},
			wantKey:    commandKeyAlarm,
			wantAction: "clear",
		},
		{
			name:       "alarm_invalid → alarm 키 + action=invalid",
			cmdType:    domain.CommandAlarmInvalid,
			params:     map[string]any{},
			wantKey:    commandKeyAlarm,
			wantAction: "invalid",
		},
		{
			name:       "alarm(action 없음) → alarm 키 + 기본 action=list",
			cmdType:    domain.CommandType("alarm"),
			params:     map[string]any{},
			wantKey:    commandKeyAlarm,
			wantAction: "list",
		},
		{
			name:           "alarm(action 있음) → alarm 키 + 기존 action 유지",
			cmdType:        domain.CommandType("alarm"),
			params:         map[string]any{"action": "custom"},
			wantKey:        commandKeyAlarm,
			wantAction:     "custom",
			wantParamsKept: true,
		},
		{
			name:       "news_subscription(action 없음) → news_subscription 키 + 기본 action=status",
			cmdType:    domain.CommandMemberNewsSubscription,
			params:     map[string]any{},
			wantKey:    commandKeyNewsSubscription,
			wantAction: "status",
		},
		{
			name:       "news_subscription_toggle → news_subscription 키 + action=toggle",
			cmdType:    domain.CommandType("news_subscription_toggle"),
			params:     map[string]any{},
			wantKey:    commandKeyNewsSubscription,
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
			{
				name:    "settlement_status → 변환 없이 원래 타입 유지",
				cmdType: domain.CommandSettlementStatus,
				params:  map[string]any{"action": "status"},
				wantKey: "settlement_status",
			},
		}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// 원본 params 복사 (불변성 검증용)
			origParams := make(map[string]any, len(tc.params))
			maps.Copy(origParams, tc.params)

			gotKey, gotParams := normalizeCommandKey(tc.cmdType, tc.params)

			if gotKey != tc.wantKey {
				t.Errorf("normalizeCommandKey(%q) key = %q, want %q", tc.cmdType, gotKey, tc.wantKey)
			}

			// action 검증 (alarm/news_subscription 계열만)
			if tc.wantAction != "" {
				gotAction, ok := gotParams["action"]
				if !ok {
					t.Fatal("gotParams에 action 키가 없음")
				}

				if gotAction != tc.wantAction {
					t.Errorf("gotParams[action] = %q, want %q", gotAction, tc.wantAction)
				}
			}

			// 기존 파라미터 유지 검증 (alarm with action 케이스)
			if tc.wantParamsKept {
				// action 변환이 일어나지 않은 경우, 원본 params와 동일 포인터여야 한다
				// (normalizeCommandKey는 타입이 bare "alarm"이고 action이 이미 있으면 params를 그대로 반환)
				for k, origV := range origParams {
					if gotV := gotParams[k]; gotV != origV {
						t.Errorf("gotParams[%q] = %v, want %v (원본 파라미터 변경됨)", k, gotV, origV)
					}
				}
			}

			// 원본 params가 변경되지 않았는지 검증
			for k, origV := range origParams {
				if tc.params[k] != origV {
					t.Errorf("원본 params[%q]가 변경됨: got %v, want %v", k, tc.params[k], origV)
				}
			}
		})
	}
}

func TestCloneParamsWithAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		params     map[string]any
		action     string
		wantAction string
		wantKeys   []string
	}{
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

			// 원본 params 스냅샷 (불변성 검증용)
			origLen := len(tc.params)

			got := cloneParamsWithAction(tc.params, tc.action)

			// action 값 검증
			if got["action"] != tc.wantAction {
				t.Errorf("got[action] = %v, want %q", got["action"], tc.wantAction)
			}

			// 기대하는 모든 키가 존재하는지 검증
			for _, key := range tc.wantKeys {
				if _, ok := got[key]; !ok {
					t.Errorf("반환된 맵에 키 %q가 없음", key)
				}
			}

			// 원본 파라미터가 변경되지 않았는지 검증
			if tc.params != nil && len(tc.params) != origLen {
				t.Errorf("원본 params 길이가 변경됨: got %d, want %d", len(tc.params), origLen)
			}

			// 반환된 맵이 원본과 다른 포인터인지 검증 (진짜 복제인지)
			if len(tc.params) > 0 {
				// 반환된 맵을 수정해도 원본에 영향 없어야 한다
				got["__test_isolation__"] = true

				if _, leaked := tc.params["__test_isolation__"]; leaked {
					t.Error("cloneParamsWithAction이 원본 맵을 반환함 (복제 아님)")
				}
			}
		})
	}
}
