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

package member

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"

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

type erroringMemberProvider struct {
	*stubMemberProvider
	err error
}

func (p *erroringMemberProvider) LoadAllMembers() ([]*domain.Member, error) {
	return nil, p.err
}

func (p *erroringMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return p
}

func TestProfileService_GetByEnglishAndChannel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	profiles, err := domain.LoadProfiles()
	if err != nil {
		t.Fatalf("failed to load profiles: %v", err)
	}

	keys := make([]string, 0, len(profiles))
	for key := range profiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var target *domain.TalentProfile
	for _, key := range keys {
		if profiles[key] != nil && profiles[key].EnglishName != "" {
			target = profiles[key]
			break
		}
	}
	if target == nil {
		t.Fatalf("no profile with english name")
	}

	provider := newStubMemberProvider([]*domain.Member{
		{Name: target.EnglishName, ChannelID: "channel-1"},
	})

	service, err := NewProfileService(nil, provider, logger)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	profile, err := service.GetByEnglish(target.EnglishName)
	if err != nil {
		t.Fatalf("GetByEnglish failed: %v", err)
	}
	if profile.Slug != target.Slug {
		t.Fatalf("unexpected slug: %s", profile.Slug)
	}

	byChannel, err := service.GetByChannel("channel-1")
	if err != nil {
		t.Fatalf("GetByChannel failed: %v", err)
	}
	if byChannel.Slug != target.Slug {
		t.Fatalf("unexpected channel slug: %s", byChannel.Slug)
	}

	withTranslation, translated, err := service.GetWithTranslation(context.Background(), target.EnglishName)
	if err != nil {
		t.Fatalf("GetWithTranslation failed: %v", err)
	}
	if withTranslation == nil || translated == nil {
		t.Fatalf("expected profile and translation")
	}
	if translated.DisplayName == "" {
		t.Fatalf("expected translated display name")
	}
}

func TestNewProfileService_ReturnsMemberLoadError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	provider := &erroringMemberProvider{
		stubMemberProvider: newStubMemberProvider(nil),
		err:                errors.New("member repo down"),
	}

	_, err = NewProfileService(nil, provider, logger)
	if err == nil {
		t.Fatal("NewProfileService() error = nil, want non-nil")
	}
	if got := err.Error(); got != "load members data: load all members: member repo down" {
		t.Fatalf("NewProfileService() error = %q, want %q", got, "load members data: load all members: member repo down")
	}
}
