package matcher

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubMemberProvider struct {
	members   []*domain.Member
	byChannel map[string]*domain.Member
	byName    map[string]*domain.Member
	byAlias   map[string]*domain.Member
}

func newStubMemberProvider(members []*domain.Member) *stubMemberProvider {
	byChannel := make(map[string]*domain.Member)
	byName := make(map[string]*domain.Member)
	byAlias := make(map[string]*domain.Member)
	for _, member := range members {
		if member == nil {
			continue
		}
		if member.ChannelID != "" {
			byChannel[member.ChannelID] = member
		}
		if member.Name != "" {
			byName[member.Name] = member
		}
		for _, alias := range member.GetAllAliases() {
			if alias != "" {
				byAlias[alias] = member
			}
		}
	}
	return &stubMemberProvider{
		members:   members,
		byChannel: byChannel,
		byName:    byName,
		byAlias:   byAlias,
	}
}

func (p *stubMemberProvider) FindMemberByChannelID(channelID string) *domain.Member {
	return p.byChannel[channelID]
}

func (p *stubMemberProvider) FindMemberByName(name string) *domain.Member {
	return p.byName[name]
}

func (p *stubMemberProvider) FindMemberByAlias(alias string) *domain.Member {
	return p.byAlias[alias]
}

func (p *stubMemberProvider) GetChannelIDs() []string {
	ids := make([]string, 0, len(p.byChannel))
	for id := range p.byChannel {
		ids = append(ids, id)
	}
	return ids
}

func (p *stubMemberProvider) GetAllMembers() []*domain.Member {
	return p.members
}

func (p *stubMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return p
}

func (p *stubMemberProvider) FindMembersByName(name string) []*domain.Member {
	return nil
}

func (p *stubMemberProvider) FindMembersByAlias(alias string) []*domain.Member {
	return nil
}

func TestCandidateFromMember(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mm := &MemberMatcher{logger: logger}

	member := &domain.Member{ChannelID: "ch1", NameJa: "jp-name"}
	candidate := mm.candidateFromMember(member, "source")
	if candidate == nil {
		t.Fatalf("expected candidate")
	}
	if candidate.channelID != "ch1" || candidate.memberName != "jp-name" {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}

	member = &domain.Member{ChannelID: "ch2"}
	candidate = mm.candidateFromMember(member, "source")
	if candidate == nil || candidate.memberName != "ch2" {
		t.Fatalf("expected channel id fallback, got: %+v", candidate)
	}
}

func TestCandidateFromDynamic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch1", Name: "member"},
	})

	mm := &MemberMatcher{logger: logger}

	candidate := mm.candidateFromDynamic(provider, "display", "ch1", "source")
	if candidate == nil || candidate.memberName != "member" {
		t.Fatalf("expected provider member, got: %+v", candidate)
	}

	candidate = mm.candidateFromDynamic(nil, "", "ch3", "source")
	if candidate == nil || candidate.memberName != "ch3" {
		t.Fatalf("expected channel id fallback, got: %+v", candidate)
	}
}

func TestTryPartialStaticMatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch1", Name: "Test Name"},
	})
	mm := &MemberMatcher{logger: logger}

	candidate := mm.tryPartialStaticMatch(provider, "test")
	if candidate == nil || candidate.channelID != "ch1" {
		t.Fatalf("expected partial match, got: %+v", candidate)
	}
}

func TestMaybeCleanupMatchCache(t *testing.T) {
	now := time.Now()
	mm := &MemberMatcher{
		matchCache: map[string]*MatchCacheEntry{
			"old": {Channel: &domain.Channel{ID: "old"}, Timestamp: now.Add(-2 * time.Minute)},
			"new": {Channel: &domain.Channel{ID: "new"}, Timestamp: now},
		},
		matchCacheTTL:         time.Minute,
		matchCacheLastCleanup: now.Add(-2 * time.Minute),
	}

	mm.maybeCleanupMatchCache()
	if _, ok := mm.matchCache["old"]; ok {
		t.Fatalf("expected old cache entry to be removed")
	}
	if _, ok := mm.matchCache["new"]; !ok {
		t.Fatalf("expected new cache entry to remain")
	}
}

func TestFinalizeCandidateFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mm := &MemberMatcher{logger: logger}

	channel, err := mm.finalizeCandidate(context.Background(), &matchCandidate{
		channelID:  "ch1",
		memberName: "name",
		source:     "source",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channel == nil || channel.ID != "ch1" {
		t.Fatalf("unexpected channel: %+v", channel)
	}
	if channel.EnglishName == nil || *channel.EnglishName != "name" {
		t.Fatalf("unexpected english name: %+v", channel.EnglishName)
	}

	channel, err = mm.finalizeCandidate(context.Background(), nil)
	if err != nil || channel != nil {
		t.Fatalf("expected nil candidate result, got: %+v, err: %v", channel, err)
	}
}

func TestToStringPtr(t *testing.T) {
	if toStringPtr("") != nil {
		t.Fatalf("expected nil for empty string")
	}
	value := toStringPtr("value")
	if value == nil || *value != "value" {
		t.Fatalf("unexpected value: %+v", value)
	}
}
