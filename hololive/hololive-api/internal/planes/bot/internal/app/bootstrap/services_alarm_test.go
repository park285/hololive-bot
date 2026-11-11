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
	"testing"

	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

// alarm provider URL이 없으면 in-process AlarmService로 대신하지 않고 기동을 실패시킨다.
func TestNewAlarmClientRequiresAlarmProviderURL(t *testing.T) {
	t.Parallel()

	client, err := newAlarmClient(&apiconfig.BotPlaneConfig{}, slog.New(slog.DiscardHandler))

	require.Nil(t, client)
	require.EqualError(t, err, "alarm provider URL (ALARM_INTERNAL_URL) is required")
}

func TestNewAlarmClientUsesAlarmWorkerURL(t *testing.T) {
	t.Parallel()

	client, err := newAlarmClient(
		&apiconfig.BotPlaneConfig{AlarmServiceURL: "http://127.0.0.1:8081"},
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, client)
}
