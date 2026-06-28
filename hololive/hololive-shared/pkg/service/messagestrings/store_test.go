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
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestStore_GetSeededValues(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

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
		{messagestrings.NamespaceMisc, "vtuber_fallback", "VTuber"},
	}
	for _, c := range cases {
		if got := store.Get(c.namespace, c.key); got != c.want {
			t.Errorf("Get(%q, %q) = %q, want %q", c.namespace, c.key, got, c.want)
		}
	}
}

func TestStore_MissingReturnsEmpty(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())

	if got := store.Get(messagestrings.NamespaceOrg, "nonexistent"); got != "" {
		t.Errorf("missing key = %q, want empty string", got)
	}
	if got := store.Get("no_such_namespace", "x"); got != "" {
		t.Errorf("missing namespace = %q, want empty string", got)
	}
}

func TestStore_NilReceiverSafe(t *testing.T) {
	var store *messagestrings.Store
	if got := store.Get(messagestrings.NamespaceOrg, "Hololive"); got != "" {
		t.Errorf("nil store Get = %q, want empty string", got)
	}
	if got := store.GetMap(messagestrings.NamespaceOrg); got != nil {
		t.Errorf("nil store GetMap = %v, want nil", got)
	}
}

func TestStore_GetMap(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())

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

func TestStore_VTuberFallbackContextReadsSSOT(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := messagestrings.NewStore(pool, slog.Default())
	ctx := context.Background()

	if got := store.VTuberFallbackContext(ctx); got != "VTuber" {
		t.Fatalf("seeded VTuberFallbackContext = %q, want VTuber", got)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE message_strings SET value = '버튜버' WHERE namespace = 'misc' AND key = 'vtuber_fallback'`,
	); err != nil {
		t.Fatalf("update vtuber_fallback row: %v", err)
	}
	store.Invalidate()

	if got := store.VTuberFallbackContext(ctx); got != "버튜버" {
		t.Errorf("VTuberFallbackContext after row update = %q, want 버튜버 (proves SSOT read, not literal)", got)
	}

	var nilStore *messagestrings.Store
	if got := nilStore.VTuberFallbackContext(ctx); got != "VTuber" {
		t.Errorf("nil store VTuberFallbackContext = %q, want VTuber literal fallback", got)
	}
}

func TestStore_InvalidateReloadsAfterMutation(t *testing.T) {
	pool := dbtest.NewPool(t)
	store := messagestrings.NewStore(pool, slog.Default())
	ctx := context.Background()

	if got := store.Get(messagestrings.NamespaceMisc, "vtuber_fallback"); got != "VTuber" {
		t.Fatalf("prime read = %q, want VTuber", got)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO message_strings(namespace, key, value) VALUES ('test', 'k', 'v')`,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if got := store.Get("test", "k"); got != "" {
		t.Errorf("cached read before invalidate = %q, want empty string", got)
	}

	store.Invalidate()

	if got := store.Get("test", "k"); got != "v" {
		t.Errorf("read after invalidate = %q, want v", got)
	}
}
