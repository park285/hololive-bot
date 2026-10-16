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

package scraper

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	defaultEventFeedURL          = "https://hololive.hololivepro.com/events/feed/"
	defaultNewsFeedURL           = "https://hololive.hololivepro.com/news/feed/"
	defaultENNewsFeedURL         = "https://hololive.hololivepro.com/en/news/feed/"
	defaultFeedUserAgent         = "hololive-llm-sched/majorevent-scraper"
	defaultMaxBodyBytes          = 4 * 1024 * 1024
	defaultIncrementalMax        = 200
	defaultFeedHTTPTimeout       = 20 * time.Second
	defaultLinkCheckerHTTPClient = 15 * time.Second
)

// FeedSource는 RSS 피드 소스 정보를 정의한다.
type FeedSource struct {
	Name      string
	EventType domain.MajorEventType
	FeedURL   string
}

// ServiceConfig는 RSS 수집 서비스 설정이다.
type ServiceConfig struct {
	Sources          []FeedSource
	FeedConcurrency  int
	IncrementalLimit int // 현재 피드 ID 존재 조회 한 번의 최대 항목 수
}

// DefaultServiceConfig는 기본 RSS 수집 설정을 반환한다.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		Sources: []FeedSource{
			{
				Name:      "event",
				EventType: domain.MajorEventTypeEvent,
				FeedURL:   defaultEventFeedURL,
			},
			{
				Name:      "news",
				EventType: domain.MajorEventTypeNews,
				FeedURL:   defaultNewsFeedURL,
			},
			{
				Name:      "en-news",
				EventType: domain.MajorEventTypeNews,
				FeedURL:   defaultENNewsFeedURL,
			},
		},
		FeedConcurrency:  3,
		IncrementalLimit: defaultIncrementalMax,
	}
}

// FeedFetcherConfig는 피드 HTTP 가져오기 설정이다.
type FeedFetcherConfig struct {
	UserAgent   string
	MaxBodySize int64
}

// DefaultFeedFetcherConfig는 기본 피드 가져오기 설정을 반환한다.
func DefaultFeedFetcherConfig() FeedFetcherConfig {
	return FeedFetcherConfig{
		UserAgent:   defaultFeedUserAgent,
		MaxBodySize: defaultMaxBodyBytes,
	}
}

// FeedScheduleConfig는 피드 스케줄러 설정이다.
type FeedScheduleConfig struct {
	ScrapeHourKST int
	RetryDelays   []time.Duration
	RunTimeout    time.Duration
}

// DefaultFeedScheduleConfig는 기본 피드 스케줄 설정을 반환한다.
func DefaultFeedScheduleConfig() FeedScheduleConfig {
	return FeedScheduleConfig{
		ScrapeHourKST: 4,
		RetryDelays: []time.Duration{
			30 * time.Minute,
			2 * time.Hour,
			6 * time.Hour,
		},
		RunTimeout: 90 * time.Second,
	}
}

// LinkCheckerConfig는 링크 검증 설정이다.
type LinkCheckerConfig struct {
	Timeout     time.Duration
	Concurrency int
}

// DefaultLinkCheckerConfig는 기본 링크 검증 설정을 반환한다.
func DefaultLinkCheckerConfig() LinkCheckerConfig {
	return LinkCheckerConfig{
		Timeout:     8 * time.Second,
		Concurrency: 8,
	}
}

// MaintenanceConfig는 유지보수 스케줄러 설정이다.
type MaintenanceConfig struct {
	ExpireHourKST     int
	LinkCheckInterval time.Duration
	RunTimeout        time.Duration
}

// DefaultMaintenanceConfig는 기본 유지보수 설정을 반환한다.
func DefaultMaintenanceConfig() MaintenanceConfig {
	return MaintenanceConfig{
		ExpireHourKST:     5,
		LinkCheckInterval: 12 * time.Hour,
		RunTimeout:        2 * time.Minute,
	}
}

// ScrapeResult는 한 번의 RSS 수집 결과 요약이다.
type ScrapeResult struct {
	FeedsAttempted int
	FeedsFailed    int
	ParsedEvents   int
	StoredEvents   int
	FailedEvents   int // 저장을 시도했지만 실패한 이벤트 수; 원인별 오류는 Scrape의 error에 포함
	SkippedKnown   int
}

// LinkCheckResult는 링크 검증 결과 요약이다.
type LinkCheckResult struct {
	Checked int
	OK      int
	Failed  int
	Blocked int
}
