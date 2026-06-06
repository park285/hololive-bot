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

package polling

import (
	"context"
	"time"
)

type BudgetSource string

const (
	BudgetSourceYouTubeScraper  BudgetSource = "youtube_scraper"
	BudgetSourceHolodexLive     BudgetSource = "holodex_live"
	BudgetSourceBrowserSnapshot BudgetSource = "browser_snapshot"
	BudgetSourceProxy           BudgetSource = "proxy"
	BudgetSourcePostgresWrite   BudgetSource = "postgres_write"
)

type BudgetProfile struct {
	SourceUnits         map[BudgetSource]float64
	FallbackSourceUnits map[BudgetSource]float64
	BurstClass          BudgetBurstClass
	Priority            BudgetPriority
}

type BudgetBurstClass string

const (
	BudgetBurstPrimary  BudgetBurstClass = "primary"
	BudgetBurstBackfill BudgetBurstClass = "backfill"
	BudgetBurstFallback BudgetBurstClass = "fallback"
)

type BudgetPriority string

const (
	BudgetPriorityHigh   BudgetPriority = "high"
	BudgetPriorityNormal BudgetPriority = "normal"
	BudgetPriorityLow    BudgetPriority = "low"
)

type BudgetJob struct {
	Namespace  string
	InstanceID string
	PollerName string
	ChannelID  string
	JobKey     string
}

type BudgetDecision struct {
	Allowed        bool
	RetryAfter     time.Duration
	Reason         string
	AffectedSource string
}

type BudgetReservation interface {
	Commit(ctx context.Context) error
	Release(ctx context.Context) error
}

type GlobalBudgetLimiter interface {
	TryReserve(ctx context.Context, job BudgetJob, profile BudgetProfile, ttl time.Duration) (BudgetReservation, BudgetDecision, error)
}

type SourceCooldownReporter interface {
	MarkSourceCooldown(ctx context.Context, source BudgetSource, ttl time.Duration, reason string) error
}

type BudgetContext struct {
	Namespace  string
	InstanceID string
	Enabled    bool
}
