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

package messagestrings_test

import (
	"log/slog"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func loadSeededStore(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	return store
}

func TestStore_LookupSeededValues(t *testing.T) {
	store := loadSeededStore(t)

	cases := []struct {
		namespace string
		key       string
		want      string
	}{
		{messagestrings.NamespaceOrg, "Hololive", "Holo"},
		{messagestrings.NamespaceOrg, "Nijisanji", "니지산지"},
		{messagestrings.NamespaceAlarmType, "LIVE", "방송"},
		{messagestrings.NamespaceAlarmType, "ANNIVERSARY", "주년"},
		{messagestrings.NamespaceNewsCat, "birthday_live", "생일 라이브"},
		{messagestrings.NamespaceNewsCat, "other", "기타"},
		{messagestrings.NamespaceSocial, "歌の再生リスト", "음악 플레이리스트"},
		{messagestrings.NamespaceMisc, "chzzk_title", "치지직 라이브"},
	}
	for _, c := range cases {
		if got, ok := store.Lookup(c.namespace, c.key); !ok || got != c.want {
			t.Errorf("Lookup(%q, %q) = (%q, %t), want (%q, true)", c.namespace, c.key, got, ok, c.want)
		}
	}

	if got := store.Text(messagestrings.MiscVTuberFallback); got != "VTuber" {
		t.Errorf("Text(%s) = %q, want VTuber", messagestrings.MiscVTuberFallback, got)
	}
}

func TestStore_LookupMissingReturnsNotFound(t *testing.T) {
	store := loadSeededStore(t)

	if got, ok := store.Lookup(messagestrings.NamespaceOrg, "nonexistent"); ok || got != "" {
		t.Errorf("missing key = (%q, %t), want (\"\", false)", got, ok)
	}

	if got, ok := store.Lookup("no_such_namespace", "x"); ok || got != "" {
		t.Errorf("missing namespace = (%q, %t), want (\"\", false)", got, ok)
	}
}

func TestStore_NilReceiverSafe(t *testing.T) {
	var store *messagestrings.Store

	if got := store.Text(messagestrings.MiscVTuberFallback); got != "" {
		t.Errorf("nil store Text = %q, want empty string", got)
	}

	if got := store.GetMap(messagestrings.NamespaceOrg); got != nil {
		t.Errorf("nil store GetMap = %v, want nil", got)
	}

	if err := store.Load(t.Context()); err == nil {
		t.Error("nil store Load must fail")
	}

	if err := store.Validate(messagestrings.Requirements{}); err == nil {
		t.Error("nil store Validate must fail")
	}
}

func TestStore_GetMap(t *testing.T) {
	store := loadSeededStore(t)

	alarmTypes := store.GetMap(messagestrings.NamespaceAlarmType)
	if len(alarmTypes) != 6 {
		t.Fatalf("alarmtype map len = %d, want 6", len(alarmTypes))
	}

	if alarmTypes["LIVE"] != "방송" {
		t.Errorf("alarmtype[LIVE] = %q, want 방송", alarmTypes["LIVE"])
	}

	if got := store.GetMap("no_such_namespace"); got != nil {
		t.Errorf("missing namespace map = %v, want nil", got)
	}
}

// 운영 runtime 계약(alarm-worker egress)은 실제 적재된 시드로 검증을 통과해야 한다.
func TestStore_SeedSatisfiesAlarmWorkerEgressRequirements(t *testing.T) {
	store := loadSeededStore(t)

	if err := store.Validate(messagestrings.AlarmWorkerEgressRequirements()); err != nil {
		t.Fatalf("Validate(alarm-worker egress) error = %v", err)
	}
}
