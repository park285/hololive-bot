package render

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCalendarCardRenderer_RenderCalendarImage_EmptyEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	data, err := r.RenderCalendarImage(6, 2026, nil)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	assertValidPNG(t, data)
}

func TestCalendarCardRenderer_RenderCalendarImage_WithEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 15, Ordinal: 3},
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{NameKo: "스이세이"}, Day: 22},
	}

	data, err := r.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	assertValidPNG(t, data)

	img, decErr := png.Decode(bytes.NewReader(data))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}

	bounds := img.Bounds()
	if bounds.Dx() != canvasWidth {
		t.Errorf("width = %d, want %d", bounds.Dx(), canvasWidth)
	}
	if bounds.Dy() <= headerH {
		t.Error("height should be larger than header for entries")
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_NilMember(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: nil, Day: 1},
	}

	data, err := r.RenderCalendarImage(1, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	assertValidPNG(t, data)
}

func TestCalendarCardRenderer_CanvasHeightCapped(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := make([]domain.CalendarEntry, 200)
	for i := range entries {
		entries[i] = domain.CalendarEntry{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{Name: "Test"},
			Day:    (i % 28) + 1,
		}
	}

	data, err := r.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	img, _ := png.Decode(bytes.NewReader(data))
	if img.Bounds().Dy() > maxCanvasH {
		t.Errorf("canvas height %d exceeds max %d", img.Bounds().Dy(), maxCanvasH)
	}
}

func TestGroupEntriesByDay(t *testing.T) {
	t.Parallel()

	entries := []domain.CalendarEntry{
		{Day: 5}, {Day: 5}, {Day: 10}, {Day: 10}, {Day: 10}, {Day: 20},
	}

	groups := groupEntriesByDay(entries)
	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d, want 3", len(groups))
	}
	if groups[0].day != 5 || len(groups[0].entries) != 2 {
		t.Errorf("group[0] = day %d, entries %d", groups[0].day, len(groups[0].entries))
	}
	if groups[1].day != 10 || len(groups[1].entries) != 3 {
		t.Errorf("group[1] = day %d, entries %d", groups[1].day, len(groups[1].entries))
	}
	if groups[2].day != 20 || len(groups[2].entries) != 1 {
		t.Errorf("group[2] = day %d, entries %d", groups[2].day, len(groups[2].entries))
	}
}

func TestEntryDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		member *domain.Member
		want   string
	}{
		{"nil member", nil, "알 수 없음"},
		{"short korean name", &domain.Member{ShortKoreanName: "페코라", NameKo: "우사다 페코라", Name: "Pekora"}, "페코라"},
		{"korean name fallback", &domain.Member{NameKo: "우사다 페코라", Name: "Pekora"}, "우사다 페코라"},
		{"english name fallback", &domain.Member{Name: "Pekora"}, "Pekora"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := entryDisplayName(tt.member); got != tt.want {
				t.Errorf("entryDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertValidPNG(t *testing.T, data []byte) {
	t.Helper()

	if len(data) == 0 {
		t.Fatal("image data is empty")
	}
	if !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("data is not a valid PNG")
	}
}
