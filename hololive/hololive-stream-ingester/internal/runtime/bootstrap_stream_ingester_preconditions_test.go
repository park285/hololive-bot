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
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func newStreamIngesterTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildStreamIngesterRuntime_Preconditions(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildStreamIngesterRuntime(context.Background(), nil, newStreamIngesterTestLogger())
		require.Error(t, err)
		assert.Nil(t, runtime)
		assert.Equal(t, "config must not be nil", err.Error())
	})

	t.Run("nil logger", func(t *testing.T) {
		cfg := &config.Config{}
		runtime, err := BuildStreamIngesterRuntime(context.Background(), cfg, nil)
		require.Error(t, err)
		assert.Nil(t, runtime)
		assert.Equal(t, "logger must not be nil", err.Error())
	})
}

func TestBuildYouTubeScraperRuntimeRequiresRuntimeAllowEnv(t *testing.T) {
	t.Setenv("YOUTUBE_SCRAPER_RUNTIME_ALLOWED", "")

	cfg := buildInfraFailureConfig()
	cfg.Ingestion.CommunityShortsBigBangEnabled = true

	runtime, err := BuildYouTubeScraperRuntime(context.Background(), cfg, newStreamIngesterTestLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "youtube scraper runtime disabled: set YOUTUBE_SCRAPER_RUNTIME_ALLOWED=true on the owning host", err.Error())
}
