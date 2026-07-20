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

package ua

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRotatingProvider_Headers_SessionTTL(t *testing.T) {
	provider := NewRotatingProvider(StrategySessionTTL, 100*time.Millisecond)
	ctx := context.Background()

	snap1 := provider.Headers(ctx)
	if snap1.UserAgent == "" {
		t.Error("UserAgent should not be empty")
	}

	// 즉시 다시 호출 - 같은 스냅샷 반환해야 함
	snap2 := provider.Headers(ctx)
	if snap1.UserAgent != snap2.UserAgent {
		t.Errorf("SessionTTL strategy should return same UA within TTL: got %q and %q", snap1.UserAgent, snap2.UserAgent)
	}
	if snap1.SecChUA != snap2.SecChUA {
		t.Errorf("SessionTTL strategy should return same SecChUA within TTL: got %q and %q", snap1.SecChUA, snap2.SecChUA)
	}

	// TTL 만료 후 - 다른 스냅샷 반환 가능
	time.Sleep(150 * time.Millisecond)
	snap3 := provider.Headers(ctx)
	if snap3.UserAgent == "" {
		t.Error("UserAgent should not be empty after TTL expiry")
	}
}

func TestRotatingProvider_Headers_PerRequest(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	seen := make(map[string]bool)
	for range 100 {
		snap := provider.Headers(ctx)
		if snap.UserAgent == "" {
			t.Error("UserAgent should not be empty")
		}
		seen[snap.UserAgent] = true
	}

	if len(seen) < 2 {
		t.Errorf("PerRequest strategy should generate varied UAs: got only %d unique UAs", len(seen))
	}
}

func TestRotatingProvider_ChromeUA_Format(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	// Chrome UA Reduction: Chrome/{major}.0.0.0 패턴
	chromePattern := regexp.MustCompile(`Chrome/\d+\.0\.0\.0`)

	hasChrome := false
	for range 50 {
		snap := provider.Headers(ctx)
		if strings.Contains(snap.UserAgent, "Chrome/") && !strings.Contains(snap.UserAgent, "Edg/") {
			hasChrome = true
			if !strings.HasPrefix(snap.UserAgent, "Mozilla/5.0 (") {
				t.Errorf("Chrome UA should start with 'Mozilla/5.0 (': got %q", snap.UserAgent)
			}
			if !chromePattern.MatchString(snap.UserAgent) {
				t.Errorf("Chrome UA should follow reduction format Chrome/{major}.0.0.0: got %q", snap.UserAgent)
			}
			if !strings.Contains(snap.UserAgent, "AppleWebKit/537.36") {
				t.Errorf("Chrome UA should contain AppleWebKit: got %q", snap.UserAgent)
			}
			break
		}
	}

	if !hasChrome {
		t.Log("Warning: No Chrome UA generated in 50 attempts (this is statistically unlikely but possible)")
	}
}

func TestRotatingProvider_FirefoxUA_Format(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	for range 200 {
		snap := provider.Headers(ctx)
		if strings.Contains(snap.UserAgent, "Firefox/") {
			if !strings.Contains(snap.UserAgent, "Gecko/20100101") {
				t.Errorf("Firefox UA should contain 'Gecko/20100101': got %q", snap.UserAgent)
			}
			if !strings.Contains(snap.UserAgent, "rv:") {
				t.Errorf("Firefox UA should contain 'rv:': got %q", snap.UserAgent)
			}
			return
		}
	}

	t.Log("Warning: No Firefox UA generated in 200 attempts (Firefox has low weight ~5%)")
}

func TestRotatingProvider_SafariUA_MacOnly(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	for range 200 {
		snap := provider.Headers(ctx)
		if strings.Contains(snap.UserAgent, "Version/") && strings.Contains(snap.UserAgent, "Safari/") {
			if !strings.Contains(snap.UserAgent, "Macintosh;") {
				t.Errorf("Safari UA should be macOS only: got %q", snap.UserAgent)
			}
			return
		}
	}

	t.Log("Warning: No Safari UA generated in 200 attempts")
}

func TestStaticProvider_Headers(t *testing.T) {
	expectedUA := "TestBot/1.0"
	provider := NewStaticProvider(expectedUA)
	ctx := context.Background()

	for range 10 {
		snap := provider.Headers(ctx)
		if snap.UserAgent != expectedUA {
			t.Errorf("StaticProvider should always return same UA: expected %q, got %q", expectedUA, snap.UserAgent)
		}
		if snap.SecChUA != "" {
			t.Errorf("StaticProvider SecChUA should be empty: got %q", snap.SecChUA)
		}
		if snap.SecChUAPlatform != "" {
			t.Errorf("StaticProvider SecChUAPlatform should be empty: got %q", snap.SecChUAPlatform)
		}
	}
}

func TestRotatingProvider_Headers_Atomicity(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	for range 100 {
		snap := provider.Headers(ctx)

		isChromium := strings.Contains(snap.UserAgent, "Chrome/") &&
			!strings.Contains(snap.UserAgent, "Firefox/") &&
			!strings.Contains(snap.UserAgent, "Version/")

		if isChromium {
			// Chromium 계열은 SecChUA가 반드시 있어야 함
			if snap.SecChUA == "" {
				t.Errorf("Chromium UA should have SecChUA: UA=%q", snap.UserAgent)
			}
			if snap.SecChUAPlatform == "" {
				t.Errorf("Chromium UA should have SecChUAPlatform: UA=%q", snap.UserAgent)
			}
		} else if snap.SecChUA != "" {
			// Firefox/Safari는 SecChUA가 비어야 함
			t.Errorf("Non-Chromium UA should not have SecChUA: UA=%q, SecChUA=%q", snap.UserAgent, snap.SecChUA)
		}
	}
}

func TestRotatingProvider_EdgeClientHints(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	for range 200 {
		snap := provider.Headers(ctx)
		if strings.Contains(snap.UserAgent, "Edg/") {
			if !strings.Contains(snap.SecChUA, "Microsoft Edge") {
				t.Errorf("Edge UA should have 'Microsoft Edge' in SecChUA: got %q", snap.SecChUA)
			}
			if strings.Contains(snap.SecChUA, "Google Chrome") {
				t.Errorf("Edge UA should NOT have 'Google Chrome' in SecChUA: got %q", snap.SecChUA)
			}
			return
		}
	}

	t.Log("Warning: No Edge UA generated in 200 attempts (Edge has low weight ~7%)")
}

func TestRotatingProvider_NonChromium_NoClientHints(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)
	ctx := context.Background()

	for range 200 {
		snap := provider.Headers(ctx)
		if strings.Contains(snap.UserAgent, "Firefox/") {
			if snap.SecChUA != "" {
				t.Errorf("Firefox should not have SecChUA: got %q", snap.SecChUA)
			}
			if snap.SecChUAPlatform != "" {
				t.Errorf("Firefox should not have SecChUAPlatform: got %q", snap.SecChUAPlatform)
			}
			return
		}
	}

	t.Log("Warning: No Firefox UA generated in 200 attempts")
}

func TestRotatingProvider_AcceptHeader(t *testing.T) {
	provider := NewRotatingProvider(StrategyPerRequest, 0)

	chromeSnap := provider.genChromeSnapshot(osWin10)
	if !strings.Contains(chromeSnap.Accept, "application/signed-exchange") {
		t.Errorf("Chrome Accept should contain signed-exchange: got %q", chromeSnap.Accept)
	}

	firefoxSnap := genFirefoxSnapshot(provider.r, osWin10)
	if strings.Contains(firefoxSnap.Accept, "application/signed-exchange") {
		t.Errorf("Firefox Accept should not contain signed-exchange: got %q", firefoxSnap.Accept)
	}

	safariSnap := genSafariSnapshot(provider.r, osMac14)
	if strings.Contains(safariSnap.Accept, "application/signed-exchange") {
		t.Errorf("Safari Accept should not contain signed-exchange: got %q", safariSnap.Accept)
	}
}

func TestPickWeighted_Distribution(t *testing.T) {
	r := newRand()

	items := []weighted[string]{
		{"A", 70},
		{"B", 20},
		{"C", 10},
	}

	counts := make(map[string]int)
	iterations := 10000

	for range iterations {
		v := pickWeighted(r, items)
		counts[v]++
	}

	aRatio := float64(counts["A"]) / float64(iterations)
	bRatio := float64(counts["B"]) / float64(iterations)
	cRatio := float64(counts["C"]) / float64(iterations)

	if aRatio < 0.65 || aRatio > 0.75 {
		t.Errorf("A ratio should be ~0.70, got %.2f", aRatio)
	}
	if bRatio < 0.15 || bRatio > 0.25 {
		t.Errorf("B ratio should be ~0.20, got %.2f", bRatio)
	}
	if cRatio < 0.05 || cRatio > 0.15 {
		t.Errorf("C ratio should be ~0.10, got %.2f", cRatio)
	}
}

func TestOSToken(t *testing.T) {
	tests := []struct {
		o        os
		expected string
	}{
		{osWin10, "Windows NT 10.0; Win64; x64"},
		{osWin11, "Windows NT 10.0; Win64; x64"},
		{osMac13, "Macintosh; Intel Mac OS X 13_6"},
		{osMac14, "Macintosh; Intel Mac OS X 14_2"},
		{osMac15, "Macintosh; Intel Mac OS X 15_0"},
		{osMac16, "Macintosh; Intel Mac OS X 16_0"},
	}

	for _, tc := range tests {
		got := osToken(tc.o)
		if got != tc.expected {
			t.Errorf("osToken(%d) = %q, expected %q", tc.o, got, tc.expected)
		}
	}
}
