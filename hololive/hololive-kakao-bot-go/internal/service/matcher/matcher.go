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

package matcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"golang.org/x/sync/singleflight"
)

// MatchCacheEntry: 멤버 매칭 결과를 캐싱하기 위한 구조체 (채널 정보 + 타임스탬프)
type MatchCacheEntry struct {
	Channel   *domain.Channel
	Timestamp time.Time
}

type matchCandidate struct {
	channelID  string
	memberName string
	org        string
	source     string
}

type snapshotEntry struct {
	candidate  *matchCandidate
	nameNorm   string
	aliasNorms []string
}

type memberMatcherSnapshot struct {
	builtAt        time.Time
	exactNames     map[string][]*snapshotEntry
	exactAliases   map[string][]*snapshotEntry
	tokenIndex     map[string][]*snapshotEntry
	entries        []*snapshotEntry
	dynamicLoadErr error
}

type snapshotMatchStrategy struct {
	name string
	find func(*memberMatcherSnapshot, string) *matchCandidate
}

// ChannelSelector: 모호한 검색어에 대해 모호성 해소를 돕는 채널 선택 인터페이스
type ChannelSelector interface {
	SelectBestChannel(ctx context.Context, query string, candidates []*domain.Channel) (*domain.Channel, error)
}

// MemberMatcher: 사용자 검색어(이름, 별명 등)를 기반으로 Hololive 멤버(채널)를 식별하고 매칭하는 서비스
// 다양한 매칭 전략(정확 일치, 부분 일치, 별명 검색 등)을 순차적으로 시도한다.
type MemberMatcher struct {
	ctx                   context.Context
	membersData           domain.MemberDataProvider
	cache                 cache.Client
	holodex               *holodex.Service
	selector              ChannelSelector
	logger                *slog.Logger
	matchCache            map[string]*MatchCacheEntry
	matchCacheMu          sync.RWMutex
	matchCacheTTL         time.Duration
	matchCacheLastCleanup time.Time
	snapshotMu            sync.RWMutex
	snapshot              *memberMatcherSnapshot
	snapshotTTL           time.Duration
	snapshotGroup         singleflight.Group
}

// NewMemberMatcher: 새로운 MemberMatcher 인스턴스를 생성합니다.
func NewMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	selector ChannelSelector,
	logger *slog.Logger,
) *MemberMatcher {
	mm := &MemberMatcher{
		ctx:                   ctx,
		membersData:           membersData,
		cache:                 cacheSvc,
		holodex:               holodexSvc,
		selector:              selector,
		logger:                logger,
		matchCache:            make(map[string]*MatchCacheEntry),
		matchCacheTTL:         1 * time.Minute,
		matchCacheLastCleanup: time.Now(),
		snapshotTTL:           1 * time.Minute,
	}

	provider := mm.providerWithContext(ctx)
	memberCount := 0
	if provider != nil {
		memberCount = len(provider.GetAllMembers())
	}

	logger.Info("MemberMatcher initialized",
		slog.Int("members", memberCount),
	)

	return mm
}

func (mm *MemberMatcher) providerWithContext(ctx context.Context) domain.MemberDataProvider {
	if mm == nil || mm.membersData == nil {
		return nil
	}
	if ctx == nil {
		return mm.membersData
	}
	return mm.membersData.WithContext(ctx)
}
