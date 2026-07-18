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
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
)

var errorKeyConstants = []string{
	messaging.ErrMemberProfileLoadFailed,
	messaging.ErrMemberProfileBuildFailed,
	messaging.ErrNoMemberInfoFound,
	messaging.ErrCannotDisplayMemberInfo,
	messaging.ErrGraduatedMemberBlocked,
	messaging.ErrAlarmServiceNotInitialized,
	messaging.ErrAlarmAddFailed,
	messaging.ErrAlarmRemoveFailed,
	messaging.ErrAlarmListFailed,
	messaging.ErrAlarmClearFailed,
	messaging.ErrAlarmNeedMemberNameAdd,
	messaging.ErrAlarmNeedMemberNameRemove,
	messaging.ErrInvalidAlarmUsage,
	messaging.ErrLiveStreamQueryFailed,
	messaging.ErrUpcomingStreamQueryFailed,
	messaging.ErrScheduleQueryFailed,
	messaging.ErrScheduleNeedMemberName,
	messaging.ErrUnknownStatsPeriod,
	messaging.ErrStatsQueryFailed,
	messaging.MsgNoStatsData,
	messaging.ErrSubscriberNeedMemberName,
	messaging.ErrSubscriberQueryFailed,
	messaging.MsgNoSubscriberData,
	messaging.ErrCalendarQueryFailed,
	messaging.ErrMajorEventServiceNotInitialized,
	messaging.ErrMajorEventStatusCheckFailed,
	messaging.ErrMajorEventSubscribeFailed,
	messaging.ErrMajorEventUnsubscribeFailed,
	messaging.ErrMemberNewsServiceNotInitialized,
	messaging.ErrMemberNewsQueryFailed,
	messaging.ErrMemberNewsSubscriptionFailed,
	messaging.ErrUnknownCommand,
	messaging.ErrExternalAPICallFailed,
	messaging.ErrCacheConnectionFailed,
	messaging.ErrIrisConnectionFailed,
	messaging.ErrCommandProcessingFailed,
}

var nonConstantErrorKeys = []string{
	"async_command_backpressure",
}

func TestErrorKeyConstantsResolveInSeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range errorKeyConstants {
		value := store.Get(messagestrings.NamespaceError, key)
		if value == "" {
			t.Errorf("error key %q has no seeded value (would degrade to sentinel at runtime)", key)
			continue
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
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := make(map[string]bool, len(errorKeyConstants)+len(nonConstantErrorKeys))
	for _, key := range errorKeyConstants {
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
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range alarmTypeKeys {
		if store.Get(messagestrings.NamespaceAlarmType, key) == "" {
			t.Errorf("alarmtype key %q has no seeded value (formatAlarmTypesLabel would silently degrade to an empty label at runtime)", key)
		}
	}
}

func TestAlarmTypeSeedHasNoOrphanKeys(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
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

var notifyKeys = []string{
	"member_news_no_members",
	"member_news_subscribed",
	"member_news_already_subscribed",
	"member_news_unsubscribed",
	"member_news_not_subscribed",
	"member_news_status_on",
	"member_news_status_off",
	"graduated_member_warning",
}

func TestNotifyKeysResolveInSeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, key := range notifyKeys {
		if store.Get(messagestrings.NamespaceNotify, key) == "" {
			t.Errorf("notify key %q has no seeded value (memberNewsNotify would silently degrade to an empty string at runtime)", key)
		}
	}
}

func TestNotifySeedHasNoOrphanKeys(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := make(map[string]bool, len(notifyKeys))
	for _, key := range notifyKeys {
		expected[key] = true
	}

	for key := range store.GetMap(messagestrings.NamespaceNotify) {
		if !expected[key] {
			t.Errorf("notify ns seed has orphan key %q with no Go consumer", key)
		}
	}
}
