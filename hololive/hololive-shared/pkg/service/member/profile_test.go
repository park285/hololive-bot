package member

import (
	"context"
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

	svc, err := NewProfileService(nil, provider, logger)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	profile, err := svc.GetByEnglish(target.EnglishName)
	if err != nil {
		t.Fatalf("GetByEnglish failed: %v", err)
	}
	if profile.Slug != target.Slug {
		t.Fatalf("unexpected slug: %s", profile.Slug)
	}

	byChannel, err := svc.GetByChannel("channel-1")
	if err != nil {
		t.Fatalf("GetByChannel failed: %v", err)
	}
	if byChannel.Slug != target.Slug {
		t.Fatalf("unexpected channel slug: %s", byChannel.Slug)
	}

	withTranslation, translated, err := svc.GetWithTranslation(context.Background(), target.EnglishName)
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
