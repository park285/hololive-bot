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

package llm

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func newMiniredisCache(t *testing.T) cache.Client {
	t.Helper()
	mini, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mini.Close)

	port, err := strconv.Atoi(mini.Port())
	require.NoError(t, err)

	svc, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:              mini.Host(),
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })
	return svc
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestValkeyCostCeiling_AccumulatesMonthlyAndWarnsOnceAtCrossing(t *testing.T) {
	cacheClient := newMiniredisCache(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	tracker := NewValkeyCostCeiling(cacheClient, 100, logger)
	require.NotNil(t, tracker)
	fixed := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return fixed }
	ctx := context.Background()

	// 60 누적 → 임계(100) 미만, 경고 없음.
	tracker.RecordUsage(ctx, "openai", "gpt-x", 60)
	require.Equal(t, 0, strings.Count(logs.String(), "llm monthly token ceiling exceeded"))

	// +60 → 120, 이번 호출에서 임계를 넘음 → 경고 1회.
	tracker.RecordUsage(ctx, "openai", "gpt-x", 60)
	require.Equal(t, 1, strings.Count(logs.String(), "llm monthly token ceiling exceeded"))

	// +10 → 130, 이미 초과 상태 → 추가 경고 없음(crossing-only).
	tracker.RecordUsage(ctx, "openai", "gpt-x", 10)
	require.Equal(t, 1, strings.Count(logs.String(), "llm monthly token ceiling exceeded"))

	// Valkey 월 카운터가 정확히 누적됐는지 직접 검증.
	got, found, err := cacheClient.GetString(ctx, "llm:cost:tokens:2026-06")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "130", got)
}

func TestValkeyCostCeiling_DisabledWhenNilCacheOrZeroCeiling(t *testing.T) {
	require.Nil(t, NewValkeyCostCeiling(nil, 100, nil))
	cacheClient := newMiniredisCache(t)
	require.Nil(t, NewValkeyCostCeiling(cacheClient, 0, nil))

	// nil tracker의 RecordUsage는 패닉 없이 no-op.
	var nilTracker *ValkeyCostCeiling
	require.NotPanics(t, func() {
		nilTracker.RecordUsage(context.Background(), "openai", "m", 100)
	})
}

func TestValkeyCostCeiling_MonthKeyIsUTCBucketed(t *testing.T) {
	tracker := &ValkeyCostCeiling{}
	require.Equal(t, "llm:cost:tokens:2026-06", tracker.monthKey(time.Date(2026, 6, 30, 23, 0, 0, 0, time.UTC)))
	require.Equal(t, "llm:cost:tokens:2026-07", tracker.monthKey(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
}
