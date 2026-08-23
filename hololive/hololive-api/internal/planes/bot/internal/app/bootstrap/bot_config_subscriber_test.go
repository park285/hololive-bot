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

package bootstrap

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingAlarmCRUD struct {
	stubAlarmCRUD
	targets     []int
	calls       int
	ctxErr      error
	ctxValue    any
	deadline    time.Time
	hasDeadline bool
}

func (r *recordingAlarmCRUD) UpdateAlarmAdvanceMinutes(ctx context.Context, _ int) []int {
	r.calls++
	r.ctxErr = ctx.Err()
	r.ctxValue = ctx.Value(bootstrapTestContextKey{})
	r.deadline, r.hasDeadline = ctx.Deadline()
	return append([]int(nil), r.targets...)
}

type recordingSettings struct {
	current settings.Settings
	updates []settings.Settings
}

func (r *recordingSettings) Get() settings.Settings {
	return r.current
}

func (r *recordingSettings) Update(newSettings settings.Settings) error {
	r.updates = append(r.updates, newSettings)
	r.current = newSettings
	return nil
}

func newExpiringBuildContext(t *testing.T) context.Context {
	t.Helper()

	ctx := context.WithValue(context.Background(), bootstrapTestContextKey{}, "build-context")
	ctx, cancel := context.WithTimeout(ctx, time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestBuildAlarmAdvanceMinutesHandlerOutlivesBuildContext(t *testing.T) {
	t.Parallel()

	alarmCRUD := &recordingAlarmCRUD{targets: []int{10, 3, 1}}
	store := &recordingSettings{current: settings.Settings{AlarmAdvanceMinutes: 5}}
	buildCtx := newExpiringBuildContext(t)

	handler := buildAlarmAdvanceMinutesHandler(
		buildCtx,
		BotConfigSubscriberDependencies{Settings: store},
		BotConfigSubscriberRuntimeDependencies{AlarmCRUD: alarmCRUD},
		slog.New(slog.DiscardHandler),
	)

	<-buildCtx.Done()
	require.ErrorIs(t, buildCtx.Err(), context.DeadlineExceeded)

	before := time.Now()
	handler(contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 10})

	require.Equal(t, 1, alarmCRUD.calls)
	assert.NoError(t, alarmCRUD.ctxErr)
	assert.Equal(t, "build-context", alarmCRUD.ctxValue)
	require.True(t, alarmCRUD.hasDeadline)
	assert.WithinDuration(t, before.Add(constants.RequestTimeout.AlarmService), alarmCRUD.deadline, 2*time.Second)

	require.Len(t, store.updates, 1)
	assert.Equal(t, 10, store.updates[0].AlarmAdvanceMinutes)
	assert.Equal(t, []int{10, 3, 1}, store.updates[0].TargetMinutes)
}

func TestBuildAlarmAdvanceMinutesHandlerSkipsPersistWhenUpdateFails(t *testing.T) {
	t.Parallel()

	alarmCRUD := &recordingAlarmCRUD{targets: []int{}}
	store := &recordingSettings{current: settings.Settings{AlarmAdvanceMinutes: 5, TargetMinutes: []int{5, 3, 1}}}

	handler := buildAlarmAdvanceMinutesHandler(
		t.Context(),
		BotConfigSubscriberDependencies{Settings: store},
		BotConfigSubscriberRuntimeDependencies{AlarmCRUD: alarmCRUD},
		slog.New(slog.DiscardHandler),
	)

	handler(contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 10})

	require.Equal(t, 1, alarmCRUD.calls)
	assert.Empty(t, store.updates)
	assert.Equal(t, 5, store.current.AlarmAdvanceMinutes)
	assert.Equal(t, []int{5, 3, 1}, store.current.TargetMinutes)
}

func TestBuildACLReloadHandlerOutlivesBuildContext(t *testing.T) {
	t.Parallel()

	service, err := ProvideACLService(
		t.Context(),
		true,
		acl.ACLModeWhitelist,
		[]string{"room-a"},
		newACLPostgresMock(t),
		cachemocks.NewLenientClient(),
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	require.NotNil(t, service)

	var logs bytes.Buffer
	buildCtx := newExpiringBuildContext(t)
	handler := buildACLReloadHandler(
		buildCtx,
		BotConfigSubscriberRuntimeDependencies{ACL: service},
		slog.New(slog.NewTextHandler(&logs, nil)),
	)

	<-buildCtx.Done()
	require.ErrorIs(t, buildCtx.Err(), context.DeadlineExceeded)

	handler(contractssettings.ACLPayloadV1{Reason: "test", Room: "room-a", Mode: "whitelist"})

	assert.Contains(t, logs.String(), "Reloaded ACL after config update")
	assert.NotContains(t, logs.String(), "Failed to reload ACL after config update")
}

func TestPersistedTargetMinutesResolvesConfiguredTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		alarmAdvanceMinutes int
		targetMinutes       []int
		want                []int
	}{
		{
			name:                "deduplicates and sorts configured targets",
			alarmAdvanceMinutes: 10,
			targetMinutes:       []int{5, 10, 5, 0, 1},
			want:                []int{10, 5, 1},
		},
		{
			name:                "single configured target expands through runtime policy",
			alarmAdvanceMinutes: 3,
			targetMinutes:       []int{10},
			want:                []int{10, 3, 1},
		},
		{
			name:                "explicit configured targets preserve configured policy",
			alarmAdvanceMinutes: 10,
			targetMinutes:       []int{10, 5, 1},
			want:                []int{10, 5, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PersistedTargetMinutes(tc.alarmAdvanceMinutes, tc.targetMinutes)

			assert.Equal(t, tc.want, got)
			require.NotEmpty(t, got)
		})
	}
}

func TestPersistedTargetMinutesFallsBackToRuntimeTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		alarmAdvanceMinutes int
		targetMinutes       []int
		want                []int
	}{
		{
			name:                "advance zero uses default targets",
			alarmAdvanceMinutes: 0,
			targetMinutes:       nil,
			want:                []int{5, 3, 1},
		},
		{
			name:                "advance one resolves to one minute",
			alarmAdvanceMinutes: 1,
			targetMinutes:       nil,
			want:                []int{1},
		},
		{
			name:                "advance two includes final minute",
			alarmAdvanceMinutes: 2,
			targetMinutes:       nil,
			want:                []int{2, 1},
		},
		{
			name:                "advance three includes final minute",
			alarmAdvanceMinutes: 3,
			targetMinutes:       []int{},
			want:                []int{3, 1},
		},
		{
			name:                "advance ten includes three and one",
			alarmAdvanceMinutes: 10,
			targetMinutes:       nil,
			want:                []int{10, 3, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PersistedTargetMinutes(tc.alarmAdvanceMinutes, tc.targetMinutes)

			assert.Equal(t, tc.want, got)
			require.NotEmpty(t, got)
		})
	}
}
