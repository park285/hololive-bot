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

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func buildInfraFailureConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Port: 30004,
		},
		Valkey: config.ValkeyConfig{
			Host: "127.0.0.1",
			Port: 1,
			DB:   0,
		},
		Postgres: config.PostgresConfig{
			Host:          "127.0.0.1",
			Port:          1,
			User:          "test",
			Password:      "test",
			Database:      "test",
			SSLMode:       "disable",
			QueryExecMode: "simple_protocol",
		},
		Notification: config.NotificationConfig{
			AdvanceMinutes: []int{5},
		},
		Holodex: config.HolodexConfig{
			BaseURL: "https://example.com",
			APIKey:  "dummy",
		},
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestInitStreamInfra_ReturnsErrorOnCanceledContext(t *testing.T) {
	t.Parallel()

	infra, err := initStreamInfra(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "provide cache resources")
}

func TestInitStreamIngesterInfrastructure_ReturnsErrorOnCanceledContext(t *testing.T) {
	t.Parallel()

	infra, err := initStreamIngesterInfrastructure(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "provide cache resources")
}

func TestBuildStreamIngesterRuntime_ReturnsErrorOnInfraInitFailure(t *testing.T) {
	t.Parallel()

	runtime, err := BuildStreamIngesterRuntime(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "provide cache resources")
}
