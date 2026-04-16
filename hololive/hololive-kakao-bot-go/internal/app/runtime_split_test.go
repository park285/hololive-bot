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

package app

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAdminAPIRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAdminAPIRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "config must not be nil", err.Error())

	runtime, err = BuildAdminAPIRuntime(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "logger must not be nil", err.Error())
}

func TestBuildAlarmWorkerRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAlarmWorkerRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "config must not be nil", err.Error())

	runtime, err = BuildAlarmWorkerRuntime(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "logger must not be nil", err.Error())
}

func TestRuntimeAllowsAlarmScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeRole string
		configValue string
		want        bool
	}{
		{name: "default bot role", runtimeRole: "bot", configValue: "", want: true},
		{name: "default worker role", runtimeRole: "worker", configValue: "", want: true},
		{name: "bot explicitly enabled", runtimeRole: "bot", configValue: "bot", want: true},
		{name: "worker explicitly enabled", runtimeRole: "worker", configValue: "worker", want: true},
		{name: "bot disabled when worker owns scheduler", runtimeRole: "bot", configValue: "worker", want: false},
		{name: "worker disabled when bot owns scheduler", runtimeRole: "worker", configValue: "bot", want: false},
		{name: "off disables all", runtimeRole: "bot", configValue: "off", want: false},
		{name: "unknown disables", runtimeRole: "worker", configValue: "mystery", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, runtimeAllowsAlarmScheduler(tt.runtimeRole, tt.configValue))
		})
	}
}
