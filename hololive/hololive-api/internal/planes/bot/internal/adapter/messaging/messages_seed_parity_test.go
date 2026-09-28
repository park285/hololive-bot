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

package messaging_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

var nonConstantErrorKeys = []string{
	"async_command_backpressure",
}

func TestErrorKeyConstantsResolveInSeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range messaging.ErrorMessageKeys() {
		value, ok := store.Lookup(messagestrings.NamespaceError, key)
		if !ok {
			t.Errorf("error key %q has no seeded value (bot plane startup validation would fail)", key)

			continue
		}

		if strings.Contains(value, "%") {
			t.Errorf("error key %q = %q, SendError passes no format args", key, value)
		}

		wantGlyph := "❌"

		if key == messaging.ErrGraduatedMemberBlocked {
			wantGlyph = "⚠️"
		}

		if !strings.HasPrefix(value, wantGlyph) {
			t.Errorf("error key %q = %q, want prefix %q", key, value, wantGlyph)
		}
	}
}

func TestErrorSeedHasNoOrphanKeys(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := make(map[string]bool, len(messaging.ErrorMessageKeys())+len(nonConstantErrorKeys))
	for _, key := range messaging.ErrorMessageKeys() {
		expected[key] = true
	}

	for _, key := range nonConstantErrorKeys {
		expected[key] = true
	}

	for key := range store.GetMap(messagestrings.NamespaceError) {
		if !expected[key] {
			t.Errorf("error ns seed has orphan key %q with no Go consumer", key)
		}
	}
}

var alarmTypeKeys = []string{
	domain.AlarmTypeLive.String(),
	domain.AlarmTypeCommunity.String(),
	domain.AlarmTypeShorts.String(),
	domain.AlarmTypeBirthday.String(),
	domain.AlarmTypeAnniversary.String(),
	"ALL",
}

func TestAlarmTypeKeysResolveInSeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range alarmTypeKeys {
		if _, ok := store.Lookup(messagestrings.NamespaceAlarmType, key); !ok {
			t.Errorf("alarmtype key %q has no seeded value (formatAlarmTypesLabel would silently degrade to an empty label at runtime)", key)
		}
	}
}

func TestAlarmTypeSeedHasNoOrphanKeys(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := make(map[string]bool, len(alarmTypeKeys))
	for _, key := range alarmTypeKeys {
		expected[key] = true
	}

	for key := range store.GetMap(messagestrings.NamespaceAlarmType) {
		if !expected[key] {
			t.Errorf("alarmtype ns seed has orphan key %q with no Go consumer", key)
		}
	}
}

func TestNotifyKeysResolveInSeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range messagestrings.NotifyKeys() {
		if store.Text(key) == "" {
			t.Errorf("notify key %s has no seeded value (bot plane startup validation would fail)", key)
		}
	}
}

func TestNotifySeedHasNoOrphanKeys(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := make(map[string]bool, len(messagestrings.NotifyKeys()))
	for _, key := range messagestrings.NotifyKeys() {
		expected[key.Name] = true
	}

	for key := range store.GetMap(messagestrings.NamespaceNotify) {
		if !expected[key] {
			t.Errorf("notify ns seed has orphan key %q with no Go consumer", key)
		}
	}
}

// formatter의 렌더 실패 문구 key는 SendError의 명령 실패 key와 같은 행이어야 한다.
func TestRenderFailureMessageKeyMatchesCommandProcessingFailed(t *testing.T) {
	if formatter.RenderFailureMessageKey.Namespace != messagestrings.NamespaceError ||
		formatter.RenderFailureMessageKey.Name != messaging.ErrCommandProcessingFailed {
		t.Fatalf("RenderFailureMessageKey = %s, want error/%s", formatter.RenderFailureMessageKey, messaging.ErrCommandProcessingFailed)
	}
}
