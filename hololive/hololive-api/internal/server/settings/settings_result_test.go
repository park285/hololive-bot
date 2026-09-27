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

import "testing"

func TestSettingsRuntimeStateResultAsMap(t *testing.T) {
	t.Parallel()

	got := (SettingsRuntimeStateResult{AlarmTargetMinutes: []int{5, 1}}).AsMap()

	if minutes, ok := got["alarm_target_minutes"].([]int); !ok || len(minutes) != 2 {
		t.Fatalf("alarm_target_minutes key mismatch: %+v", got)
	}

	// 퇴역한 scraper proxy runtime 키는 다시 노출하지 않는다.
	for _, key := range []string{"requested", "youtube_enabled", "holodex_enabled", "scheduler_enabled", "scheduler_known"} {
		if _, exists := got[key]; exists {
			t.Fatalf("retired key %q should not exist: %+v", key, got)
		}
	}
}

func TestMemberNewsWeeklyRunNowResultAsMap(t *testing.T) {
	t.Parallel()

	got := (MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "not configured",
	}).AsMap()

	if got["applied"] != false {
		t.Fatalf("applied key mismatch: %+v", got)
	}

	if got["reason"] != "not configured" {
		t.Fatalf("reason key mismatch: %+v", got)
	}
}
