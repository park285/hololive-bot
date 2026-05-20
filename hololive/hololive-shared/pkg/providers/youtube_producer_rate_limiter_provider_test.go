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

package providers

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestProvideYouTubeProducerRateLimiter_DisabledDistributed_AllowsNilCache(t *testing.T) {
	original := constants.YouTubeProducerDistributedRateLimitConfig
	t.Cleanup(func() {
		constants.YouTubeProducerDistributedRateLimitConfig = original
	})

	constants.YouTubeProducerDistributedRateLimitConfig.Enabled = false

	limiter, err := ProvideYouTubeProducerRateLimiter(nil, nil)
	if err != nil {
		t.Fatalf("ProvideYouTubeProducerRateLimiter() error = %v, want nil", err)
	}
	if limiter == nil {
		t.Fatal("ProvideYouTubeProducerRateLimiter() limiter is nil")
	}
}

func TestProvideYouTubeProducerRateLimiter_EnabledDistributed_RequiresCache(t *testing.T) {
	original := constants.YouTubeProducerDistributedRateLimitConfig
	t.Cleanup(func() {
		constants.YouTubeProducerDistributedRateLimitConfig = original
	})

	constants.YouTubeProducerDistributedRateLimitConfig.Enabled = true

	limiter, err := ProvideYouTubeProducerRateLimiter(nil, nil)
	if err == nil {
		t.Fatal("ProvideYouTubeProducerRateLimiter() expected error, got nil")
	}
	if limiter != nil {
		t.Fatal("ProvideYouTubeProducerRateLimiter() limiter must be nil on error")
	}
	if !strings.Contains(err.Error(), "initialize youtube producer distributed rate limiter") {
		t.Fatalf("ProvideYouTubeProducerRateLimiter() error = %q, want contains %q", err.Error(), "initialize youtube producer distributed rate limiter")
	}
}
