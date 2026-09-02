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
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	membermocks "github.com/kapu/hololive-api/internal/service/member/mocks"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestInitAlarmDependenciesReturnsErrorWhenCacheIsNil(t *testing.T) {
	t.Parallel()

	deps, err := InitAlarmDependencies(
		settings.ChzzkConfig{},
		&settings.TwitchConfig{},
		filepath.Join(t.TempDir(), "settings.json"),
		[]int{10, 5, 1},
		false,
		nil,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)

	require.Nil(t, deps)
	require.EqualError(t, err, "provide alarm service: failed to create alarm service: new alarm service: cache client is nil")
}

func TestInitAlarmDependenciesBuildsAlarmDependencies(t *testing.T) {
	t.Parallel()

	memberProvider := &membermocks.DataProvider{}

	deps, err := InitAlarmDependencies(
		settings.ChzzkConfig{},
		&settings.TwitchConfig{},
		filepath.Join(t.TempDir(), "settings.json"),
		[]int{10, 5, 1},
		false,
		cachemocks.NewLenientClient(),
		nil,
		memberProvider,
		nil,
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, deps)
	require.NotNil(t, deps.AlarmService)
	assert.Same(t, memberProvider, deps.MemberDataProvider)
	assert.NotNil(t, deps.ChzzkClient)
	assert.NotNil(t, deps.TwitchClient)
}

func TestInitAlarmModeComponentsWrapsAlarmServiceAsCRUD(t *testing.T) {
	t.Parallel()

	memberProvider := &membermocks.DataProvider{}
	infra := (&sharedInfraForBootstrapTest{
		cacheClient: cachemocks.NewLenientClient(),
		postgres:    &databasemocks.Client{},
	}).module()

	components, err := InitAlarmModeComponents(
		t.Context(),
		&settings.Config{
			Notification: settings.NotificationConfig{AdvanceMinutes: []int{10, 5, 1}},
		},
		infra,
		nil,
		memberProvider,
		nil,
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, components)
	require.NotNil(t, components.AlarmService)
	assert.Same(t, components.AlarmService, components.AlarmCRUD)
	assert.Same(t, memberProvider, components.MemberDataSource)
	assert.NotNil(t, components.ChzzkClient)
	assert.NotNil(t, components.TwitchClient)
}
