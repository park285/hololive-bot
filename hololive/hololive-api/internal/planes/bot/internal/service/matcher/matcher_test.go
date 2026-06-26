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
	logger := slog.New(slog.DiscardHandler)
	mm := &Matcher{logger: logger}

	member := &domain.Member{ChannelID: "ch1", NameJa: "jp-name"}

	candidate := mm.candidateFromMember(member, "source")
	if candidate == nil {
		t.Fatal("expected candidate")
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
	logger := slog.New(slog.DiscardHandler)
	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch1", Name: "member"},
	})

	mm := &Matcher{logger: logger}

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
	logger := slog.New(slog.DiscardHandler)
	provider := newStubMemberProvider([]*domain.Member{
		{ChannelID: "ch1", Name: "Test Name"},
	})
	mm := &Matcher{logger: logger}

	candidate := mm.tryPartialStaticMatch(provider, "test")
	if candidate == nil || candidate.channelID != "ch1" {
		t.Fatalf("expected partial match, got: %+v", candidate)
	}
}

func TestMaybeCleanupMatchCache(t *testing.T) {
	now := time.Now()
	mm := &Matcher{
		matchCache: map[string]*MatchCacheEntry{
			"old": {Channel: &domain.Channel{ID: "old"}, Timestamp: now.Add(-2 * time.Minute)},
			"new": {Channel: &domain.Channel{ID: "new"}, Timestamp: now},
		},
		matchCacheTTL:         time.Minute,
		matchCacheLastCleanup: now.Add(-2 * time.Minute),
	}

	mm.maybeCleanupMatchCache()

	if _, ok := mm.matchCache["old"]; ok {
		t.Fatal("expected old cache entry to be removed")
	}

	if _, ok := mm.matchCache["new"]; !ok {
		t.Fatal("expected new cache entry to remain")
	}
}

func TestFinalizeCandidateFallback(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	mm := &Matcher{logger: logger}

	channel := mm.finalizeCandidate(t.Context(), &matchCandidate{
		channelID:  "ch1",
		memberName: "name",
		source:     "source",
	})

	if channel == nil || channel.ID != "ch1" {
		t.Fatalf("unexpected channel: %+v", channel)
	}

	if channel.EnglishName == nil || *channel.EnglishName != "name" {
		t.Fatalf("unexpected english name: %+v", channel.EnglishName)
	}

	channel = mm.finalizeCandidate(t.Context(), nil)
	if channel != nil {
		t.Fatalf("expected nil candidate result, got: %+v", channel)
	}
}

func TestToStringPtr(t *testing.T) {
	if toStringPtr("") != nil {
		t.Fatal("expected nil for empty string")
	}

	value := toStringPtr("value")
	if value == nil || *value != "value" {
		t.Fatalf("unexpected value: %+v", value)
	}
}
