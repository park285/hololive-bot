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

package checker

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

func TestChzzkCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	chzzkChecker, err := NewChzzkChecker(
		cacheSvc,
		chzzk.NewClient(&http.Client{Timeout: time.Second}, chzzk.DefaultBaseURL, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewChzzkChecker() error = %v", err)
	}

	notifications, checkErr := chzzkChecker.Check(context.Background())
	if checkErr != nil {
		t.Fatalf("Check() error = %v", checkErr)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected empty notifications, got %d", len(notifications))
	}
}

func TestTwitchCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	twitchChecker, err := NewTwitchChecker(
		cacheSvc,
		twitch.NewClient(twitch.ClientConfig{}, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewTwitchChecker() error = %v", err)
	}

	notifications, checkErr := twitchChecker.Check(context.Background())
	if checkErr != nil {
		t.Fatalf("Check() error = %v", checkErr)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected empty notifications, got %d", len(notifications))
	}
}
