package matcher

import (
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const orgHololive = "Hololive"

const matchCacheMaxEntries = 1024

type MatchCacheEntry struct {
	Channel   *domain.Channel
	Timestamp time.Time
	snapshot  *matcherSnapshot
}

type matchCandidate struct {
	memberID        int
	photo           string
	graduated       bool
	channelID       string
	memberName      string
	koreanName      string
	shortKoreanName string
	org             string
	source          string
}

type snapshotEntry struct {
	candidate  *matchCandidate
	nameNorm   string
	aliasNorms []string
}

type matcherSnapshot struct {
	builtAt      time.Time
	exactNames   map[string][]*snapshotEntry
	exactAliases map[string][]*snapshotEntry
	byChannel    map[string]*snapshotEntry
	entries      []*snapshotEntry
}

type snapshotMatchStrategy struct {
	name string
	find func(*matcherSnapshot, string) *matchCandidate
}

type Matcher struct {
	membersData           domain.MemberDataProvider
	logger                *slog.Logger
	matchCache            map[string]*MatchCacheEntry
	matchCacheMu          sync.RWMutex
	matchCacheTTL         time.Duration
	matchCacheLastCleanup time.Time
	snapshotMu            sync.RWMutex
	snapshot              *matcherSnapshot
	snapshotTTL           time.Duration
	snapshotGroup         singleflight.Group
}

func NewMatcher(
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
) *Matcher {
	mm := &Matcher{
		membersData:           membersData,
		logger:                logger,
		matchCache:            make(map[string]*MatchCacheEntry),
		matchCacheTTL:         1 * time.Minute,
		matchCacheLastCleanup: time.Now(),
		snapshotTTL:           1 * time.Minute,
	}

	logger.Info("Matcher initialized")

	return mm
}
