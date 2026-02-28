package membernews

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubMemberDataProvider struct {
	channelIDs []string
}

func (s *stubMemberDataProvider) FindMemberByChannelID(_ string) *domain.Member { return nil }
func (s *stubMemberDataProvider) FindMemberByName(_ string) *domain.Member       { return nil }
func (s *stubMemberDataProvider) FindMemberByAlias(_ string) *domain.Member      { return nil }
func (s *stubMemberDataProvider) GetChannelIDs() []string {
	return append([]string(nil), s.channelIDs...)
}
func (s *stubMemberDataProvider) GetAllMembers() []*domain.Member { return nil }
func (s *stubMemberDataProvider) WithContext(_ context.Context) domain.MemberDataProvider {
	return s
}
func (s *stubMemberDataProvider) FindMembersByName(_ string) []*domain.Member  { return nil }
func (s *stubMemberDataProvider) FindMembersByAlias(_ string) []*domain.Member { return nil }

func TestSourceValidator_XAllowlistAndDomainValidation(t *testing.T) {
	tempDir := t.TempDir()
	allowlistPath := filepath.Join(tempDir, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte(`{"official_accounts":["hololivetv"]}`), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	validator, err := NewSourceValidator(allowlistPath, nil, nil)
	if err != nil {
		t.Fatalf("new source validator: %v", err)
	}

	tier, _, err := validator.ValidateSourceURL("https://hololive.hololivepro.com/news")
	if err != nil {
		t.Fatalf("official domain validate error: %v", err)
	}
	if tier != SourceTierOfficial {
		t.Fatalf("expected official tier, got %s", tier)
	}

	tier, _, err = validator.ValidateSourceURL("https://prtimes.jp/main/html/rd/p/000000001.000000000.html")
	if err != nil {
		t.Fatalf("media domain validate error: %v", err)
	}
	if tier != SourceTierMedia {
		t.Fatalf("expected media tier, got %s", tier)
	}

	_, _, err = validator.ValidateSourceURL("https://x.com/not_allowed/status/1")
	if err == nil {
		t.Fatalf("expected x account allowlist error")
	}

	tier, _, err = validator.ValidateSourceURL("https://x.com/hololivetv/status/1")
	if err != nil {
		t.Fatalf("allowed x account validate error: %v", err)
	}
	if tier != SourceTierOfficial {
		t.Fatalf("expected x account to be official, got %s", tier)
	}
}

func TestSourceValidator_YouTubeOfficialChannelClassification(t *testing.T) {
	memberData := &stubMemberDataProvider{channelIDs: []string{"UC_TEST_OFFICIAL"}}
	validator, err := NewSourceValidator("", memberData, nil)
	if err != nil {
		t.Fatalf("new source validator: %v", err)
	}

	tier, _, err := validator.ValidateSourceURL("https://www.youtube.com/channel/UC_TEST_OFFICIAL")
	if err != nil {
		t.Fatalf("youtube official validate error: %v", err)
	}
	if tier != SourceTierOfficial {
		t.Fatalf("expected official tier for allowed channel, got %s", tier)
	}

	tier, _, err = validator.ValidateSourceURL("https://www.youtube.com/channel/UC_UNKNOWN")
	if err != nil {
		t.Fatalf("youtube unknown channel validate error: %v", err)
	}
	if tier != SourceTierCommunity {
		t.Fatalf("expected community tier for unknown channel, got %s", tier)
	}

	// 채널 식별 불가한 YouTube 동영상 링크 → community
	tier, _, err = validator.ValidateSourceURL("https://youtu.be/dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("youtu.be validate error: %v", err)
	}
	if tier != SourceTierCommunity {
		t.Fatalf("expected community tier for youtu.be short link (unverifiable channel), got %s", tier)
	}

	tier, _, err = validator.ValidateSourceURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("youtube watch validate error: %v", err)
	}
	if tier != SourceTierCommunity {
		t.Fatalf("expected community tier for youtube watch link (unverifiable channel), got %s", tier)
	}
}

func TestSourceValidator_HasCorroboration(t *testing.T) {
	validator, err := NewSourceValidator("", nil, nil)
	if err != nil {
		t.Fatalf("new source validator: %v", err)
	}

	if !validator.HasCorroboration("참고: https://hololive.hololivepro.com/news/123") {
		t.Fatalf("expected corroboration to be true")
	}
	if validator.HasCorroboration("비공식 글만 있음") {
		t.Fatalf("expected corroboration to be false")
	}
}
