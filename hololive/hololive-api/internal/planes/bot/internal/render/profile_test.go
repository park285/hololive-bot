package render

import (
	"bytes"
	"context"
	"image/png"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func profileFixture() ProfileCardData {
	return NewProfileCardData(
		&domain.Member{ChannelID: "ch-pekora", IsGraduated: false},
		&domain.TalentProfile{
			EnglishName:  "Usada Pekora",
			JapaneseName: "兎田ぺこら",
			Catchphrase:  "こんぺこ！こんぺこ！こんぺこ！",
			DataEntries: []domain.TalentProfileEntry{
				{Label: "Birthday", Value: "January 12"},
				{Label: "Debut", Value: "July 17, 2019"},
			},
		},
		&domain.Translated{
			DisplayName: "우사다 페코라",
			Catchphrase: "콘페코! 콘페코! 콘페코!",
			Data: []domain.TranslatedProfileDataRow{
				{Label: "생일", Value: "1월 12일"},
				{Label: "데뷔", Value: "2019년 7월 17일"},
				{Label: "일러스트레이터", Value: "Yuuki Hagure"},
			},
		},
	)
}

func TestNewProfileCardData_TranslationPrecedence(t *testing.T) {
	t.Parallel()

	data := profileFixture()
	if data.DisplayName != "우사다 페코라" {
		t.Errorf("DisplayName = %q, want translated", data.DisplayName)
	}
	if len(data.SubNames) != 2 || data.SubNames[0] != "Usada Pekora" || data.SubNames[1] != "兎田ぺこら" {
		t.Errorf("SubNames = %v, want [EN JP]", data.SubNames)
	}
	if data.Catchphrase != "콘페코! 콘페코! 콘페코!" {
		t.Errorf("Catchphrase = %q, want translated", data.Catchphrase)
	}
	if len(data.Rows) != 3 || data.Rows[0].Label != "생일" {
		t.Errorf("Rows = %v, want translated rows", data.Rows)
	}
}

func TestNewProfileCardData_RawFallback(t *testing.T) {
	t.Parallel()

	data := NewProfileCardData(nil, &domain.TalentProfile{
		EnglishName: "Tokino Sora",
		DataEntries: []domain.TalentProfileEntry{{Label: "Birthday", Value: "May 15"}},
	}, nil)
	if data.DisplayName != "Tokino Sora" {
		t.Errorf("DisplayName = %q, want raw english", data.DisplayName)
	}
	if len(data.Rows) != 1 || data.Rows[0].Label != "Birthday" {
		t.Errorf("Rows = %v, want raw entries", data.Rows)
	}
}

func TestProfileCardRenderer_RenderProfileImage(t *testing.T) {
	t.Parallel()

	pages, err := NewProfileCardRenderer().RenderProfileImage(profileFixture())
	if err != nil {
		t.Fatalf("RenderProfileImage() error = %v", err)
	}
	assertValidPNG(t, pages)

	img, decErr := png.Decode(bytes.NewReader(pages))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}
	if img.Bounds().Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", img.Bounds().Dx(), calendarOutputWidth)
	}
}

func TestProfileCardRenderer_RenderProfileImage_EmptyNameErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewProfileCardRenderer().RenderProfileImage(ProfileCardData{}); err == nil {
		t.Fatal("RenderProfileImage(empty) error = nil, want error")
	}
}

func TestProfileCardRenderer_CachesByContent(t *testing.T) {
	t.Parallel()

	r := NewProfileCardRenderer()
	data := profileFixture()

	first, err := r.RenderProfileImage(data)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	first[0] = 0

	second, err := r.RenderProfileImage(data)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	assertValidPNG(t, second)

	changed := data
	changed.Graduated = true
	if profileCardCacheKey(changed) == profileCardCacheKey(data) {
		t.Fatal("cache key must change when content changes")
	}
}

func TestProfileStrings_SeededRowMatchesFallbackLiteral(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	if got := store.Get(messagestrings.NamespaceProfileCard, "badge_graduated"); got != "졸업" {
		t.Errorf("seeded profilecard/badge_graduated = %q, want %q", got, "졸업")
	}

	m := newProfileMetrics()
	if got := m.profileGraduatedBadge(); got != "졸업" {
		t.Errorf("nil-store fallback = %q, want %q", got, "졸업")
	}
}
