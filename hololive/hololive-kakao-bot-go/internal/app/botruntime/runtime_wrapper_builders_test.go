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

package botruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBIntegrationRuntimeClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &DBIntegrationRuntime{
		Managed: lifecycle.NewManaged(func() { calls++ }),
	}

	runtime.Close()
	assert.Equal(t, 1, calls)
}

func TestBuildDBIntegrationRuntime_ReturnsErrorOnNilLogger(t *testing.T) {
	t.Parallel()

	runtime, err := BuildDBIntegrationRuntime(t.Context(), config.PostgresConfig{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "logger must not be nil")
}

func TestFetchProfilesRuntimeClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &FetchProfilesRuntime{
		Managed: lifecycle.NewManaged(func() { calls++ }),
	}

	runtime.Close()
	assert.Equal(t, 1, calls)
}

func TestBuildFetchProfilesRuntime_WithNilContext(t *testing.T) {
	t.Parallel()

	runtime, err := BuildFetchProfilesRuntime(context.Background())
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.NotNil(t, runtime.Logger)
	require.NotNil(t, runtime.HTTPClient)
	assert.Equal(t, config.DefaultOfficialProfileConfig().RequestTimeout, runtime.HTTPClient.Timeout)
	assert.NotNil(t, runtime.HTTPClient.Transport)

	runtime.Close()
}

func TestBuildDBIntegrationRuntime_InitializesContextWhenNil(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := BuildDBIntegrationRuntime(context.Background(), config.PostgresConfig{}, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to initialize DB integration runtime")
}
