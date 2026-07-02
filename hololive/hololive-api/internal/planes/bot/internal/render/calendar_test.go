package render

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCalendarCardRenderer_RenderCalendarImage_EmptyEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	pages, err := r.RenderCalendarImages(6, 2026, nil)
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	assertValidPNG(t, pages[0])
}

func TestCalendarCardRenderer_RenderCalendarImage_WithEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 15, Ordinal: 3},
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{NameKo: "스이세이"}, Day: 22},
	}

	pages, err := r.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	assertValidPNG(t, pages[0])

	img, decErr := png.Decode(bytes.NewReader(pages[0]))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}

	bounds := img.Bounds()
	if bounds.Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", bounds.Dx(), calendarOutputWidth)
	}
	outputHeaderH := newCalendarMetrics().headerH * calendarOutputWidth / canvasWidth
	if bounds.Dy() <= outputHeaderH {
		t.Error("height should be larger than header for entries")
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_UsesTransportFriendlyCanvas(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	pages, err := r.RenderCalendarImages(6, 2026, []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
	})
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}

	img, decErr := png.Decode(bytes.NewReader(pages[0]))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}

	// 최종 출력은 카카오 표시폭에 맞춘 calendarOutputWidth로 고정한다.
	// 내부는 고해상도(canvasWidth)로 그린 뒤 이 폭으로 SSAA 다운스케일하므로,
	// 카카오가 인라인 표시 시 추가 다운스케일/재압축을 거의 하지 않는다.
	if got, want := img.Bounds().Dx(), calendarOutputWidth; got != want {
		t.Fatalf("output width = %d, want %d (kakao display width)", got, want)
	}
}

func TestDrawCircularImageAvoidsLegacySoftBilinearDownsample(t *testing.T) {
	t.Parallel()

	src := detailedAvatarSource()
	r := newCalendarMetrics().avatarSize / 2
	size := r*2 + 8
	got := image.NewRGBA(image.Rect(0, 0, size, size))
	legacy := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2

	drawCircularImage(got, src, cx, cy, r, colWhite)
	drawLegacyBilinearCircularImage(legacy, src, cx, cy, r, colWhite)

	if bytes.Equal(got.Pix, legacy.Pix) {
		t.Fatal("avatar renderer still matches legacy bilinear downsample")
	}
	if gotEnergy, legacyEnergy := avatarEdgeEnergy(got), avatarEdgeEnergy(legacy); gotEnergy <= legacyEnergy {
		t.Fatalf("avatar edge energy = %.2f, want more than legacy %.2f", gotEnergy, legacyEnergy)
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_ReusesMonthlyCache(t *testing.T) {
	var requests atomic.Int32
	server := newPNGServer(t, &requests)
	defer server.Close()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "페코라", Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	first, err := r.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImages() error = %v", err)
	}
	first[0][0] = 0

	second, err := r.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImages() error = %v", err)
	}

	assertValidPNG(t, second[0])
	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_ReusesDiskCacheAcrossRenderers(t *testing.T) {
	var requests atomic.Int32
	server := newPNGServer(t, &requests)
	defer server.Close()

	dir := t.TempDir()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "페코라", Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	firstRenderer := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	first, err := firstRenderer.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImages() error = %v", err)
	}
	assertValidPNG(t, first[0])

	secondRenderer := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	second, err := secondRenderer.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImages() error = %v", err)
	}
	assertValidPNG(t, second[0])

	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestStoreDiskCachedImage_PrunesStaleMonthHashes(t *testing.T) {
	dir := t.TempDir()
	r := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	pngBytes := tinyPNG(t)

	old := calendarCacheKey{year: 2026, month: 6, entriesHash: "old"}
	fresh := calendarCacheKey{year: 2026, month: 6, entriesHash: "new"}
	otherMonth := calendarCacheKey{year: 2026, month: 7, entriesHash: "keep"}

	r.storeDiskCachedImages(old, [][]byte{pngBytes, pngBytes})
	r.storeDiskCachedImages(otherMonth, [][]byte{pngBytes})
	r.storeDiskCachedImages(fresh, [][]byte{pngBytes, pngBytes})

	if _, ok := r.diskCachedImages(old); ok {
		t.Fatal("stale same-month hash should be pruned")
	}
	if pages, ok := r.diskCachedImages(fresh); !ok || len(pages) != 2 {
		t.Fatalf("latest same-month hash should remain with 2 pages, got ok=%v len=%d", ok, len(pages))
	}
	if _, ok := r.diskCachedImages(otherMonth); !ok {
		t.Fatal("other-month entry must not be pruned")
	}
}

func TestStoreDiskCachedImages_PartialWriteIsFullMiss(t *testing.T) {
	dir := t.TempDir()
	r := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	pngBytes := tinyPNG(t)

	key := calendarCacheKey{year: 2026, month: 6, entriesHash: "abc"}
	r.storeDiskCachedImages(key, [][]byte{pngBytes, pngBytes})

	if err := os.Remove(r.diskCachePagePath(key, 1)); err != nil {
		t.Fatalf("remove p1: %v", err)
	}
	if _, ok := r.diskCachedImages(key); ok {
		t.Fatal("missing p1 (commit marker) must invalidate the whole page set")
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_CoalescesConcurrentMonthlyCacheMisses(t *testing.T) {
	var requests atomic.Int32
	server := newDelayedPNGServer(t, &requests, 25*time.Millisecond)
	defer server.Close()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "페코라", Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	const workers = 5
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			pages, err := r.RenderCalendarImages(6, 2026, entries)
			if err != nil {
				errs <- err
				return
			}
			if len(pages) == 0 || !bytes.HasPrefix(pages[0], []byte{0x89, 'P', 'N', 'G'}) {
				errs <- errors.New("rendered data is not a valid PNG")
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("RenderCalendarImage() error = %v", err)
		}
	}
	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_RefreshesCacheWhenEntriesChange(t *testing.T) {
	var requests atomic.Int32
	server := newPNGServer(t, &requests)
	defer server.Close()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "페코라", Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}
	changedEntries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "미코", Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	if _, err := r.RenderCalendarImages(6, 2026, entries); err != nil {
		t.Fatalf("first RenderCalendarImages() error = %v", err)
	}
	if _, err := r.RenderCalendarImages(6, 2026, changedEntries); err != nil {
		t.Fatalf("changed RenderCalendarImages() error = %v", err)
	}

	if got, want := requests.Load(), int32(2); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestFetchMemberPhotoRequestsHighResolutionThumbnail(t *testing.T) {
	var requestedPath string
	pngData := tinyPNG(t)
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedPath = req.URL.Path
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	})))

	photos := make(map[string]image.Image)
	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; !ok {
		t.Fatal("photo was not stored")
	}
	if !strings.Contains(requestedPath, "=s1024-") {
		t.Fatalf("requested path = %q, want high resolution thumbnail", requestedPath)
	}
}

func TestFetchImageAcceptsLargeThumbnailPayload(t *testing.T) {
	pngData := largePNG(t)
	if got, want := len(pngData), 512*1024; got <= want {
		t.Fatalf("test PNG size = %d, want larger than %d", got, want)
	}

	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	})))

	img, err := fetchImage("https://yt3.googleusercontent.com/avatar=s1024-c")
	if err != nil {
		t.Fatalf("fetchImage() error = %v", err)
	}
	if img == nil {
		t.Fatal("fetchImage() returned nil image")
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Fatalf("image bounds = %v, want non-empty image", img.Bounds())
	}
}

func TestCalendarCacheKeyDistinguishesDelimiterCharacters(t *testing.T) {
	left := []domain.CalendarEntry{
		{
			Kind: domain.CelebrationKindBirthday,
			Day:  1,
			Member: &domain.Member{
				ID:              7,
				ChannelID:       "chan|alpha",
				Name:            "name",
				NameKo:          "ko",
				ShortKoreanName: "short",
				Photo:           "photo",
			},
		},
	}
	right := []domain.CalendarEntry{
		{
			Kind: domain.CelebrationKindBirthday,
			Day:  1,
			Member: &domain.Member{
				ID:              7,
				ChannelID:       "chan",
				Name:            "alpha|name",
				NameKo:          "ko",
				ShortKoreanName: "short",
				Photo:           "photo",
			},
		},
	}

	leftKey := newCalendarCacheKey(6, 2026, left)
	rightKey := newCalendarCacheKey(6, 2026, right)
	if leftKey == rightKey {
		t.Fatal("cache keys should differ for delimiter-containing fields")
	}
}

func TestCalendarCanvasPixelBudget(t *testing.T) {
	if got, want := canvasWidth*maxCanvasH, maxCanvasPixels; got > want {
		t.Fatalf("canvas pixel budget = %d, want at most %d", got, want)
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_NilMember(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: nil, Day: 1},
	}

	pages, err := r.RenderCalendarImages(1, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}

	assertValidPNG(t, pages[0])
}

func TestCalendarCardRenderer_PaginatesAndCapsPageCount(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := make([]domain.CalendarEntry, 200)
	for i := range entries {
		entries[i] = domain.CalendarEntry{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{Name: "Test"},
			Day:    (i / 8) + 1,
		}
	}

	pages, err := r.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}

	if len(pages) != calendarMaxPages {
		t.Fatalf("len(pages) = %d, want cap %d", len(pages), calendarMaxPages)
	}
	for i, page := range pages {
		img, decErr := png.Decode(bytes.NewReader(page))
		if decErr != nil {
			t.Fatalf("page %d Decode() error = %v", i+1, decErr)
		}
		if img.Bounds().Dx() != calendarOutputWidth {
			t.Errorf("page %d width = %d, want %d", i+1, img.Bounds().Dx(), calendarOutputWidth)
		}
		if img.Bounds().Dy() > maxCanvasH {
			t.Errorf("page %d height %d exceeds max %d", i+1, img.Bounds().Dy(), maxCanvasH)
		}
	}
}

func TestPaginateDayGroups(t *testing.T) {
	t.Parallel()

	m := newCalendarMetrics()

	t.Run("empty month yields single empty page", func(t *testing.T) {
		t.Parallel()
		pages, omitted := paginateDayGroups(&m, nil)
		if len(pages) != 1 || len(pages[0]) != 0 || omitted != 0 {
			t.Fatalf("pages = %d, first len = %d, omitted = %d; want 1 empty page", len(pages), len(pages[0]), omitted)
		}
	})

	t.Run("splits only at day boundaries within budget", func(t *testing.T) {
		t.Parallel()
		groups := make([]dayGroup, 12)
		for i := range groups {
			groups[i] = dayGroup{day: i + 1, entries: []domain.CalendarEntry{{Day: i + 1}}}
		}
		pages, omitted := paginateDayGroups(&m, groups)
		if omitted != 0 {
			t.Fatalf("omitted = %d, want 0", omitted)
		}
		if len(pages) < 2 {
			t.Fatalf("len(pages) = %d, want >= 2 for 12 single-entry groups", len(pages))
		}
		var total, prevDay int
		for _, page := range pages {
			h := m.headerH + separatorH + m.paddingY
			for _, g := range page {
				if g.day <= prevDay {
					t.Fatalf("day order broken: %d after %d", g.day, prevDay)
				}
				prevDay = g.day
				total += len(g.entries)
				h += m.dateHeaderH + len(g.entries)*m.entryRowH + m.dateSectGap
			}
			if len(page) > 1 && h+m.paddingY > calendarPageInnerH {
				t.Fatalf("multi-group page height %d exceeds budget %d", h, calendarPageInnerH)
			}
		}
		if total != 12 {
			t.Fatalf("total entries across pages = %d, want 12", total)
		}
	})

	t.Run("single oversized group gets its own page", func(t *testing.T) {
		t.Parallel()
		big := dayGroup{day: 1, entries: make([]domain.CalendarEntry, 20)}
		small := dayGroup{day: 2, entries: []domain.CalendarEntry{{Day: 2}}}
		pages, omitted := paginateDayGroups(&m, []dayGroup{big, small})
		if omitted != 0 {
			t.Fatalf("omitted = %d, want 0", omitted)
		}
		if len(pages) != 2 || len(pages[0]) != 1 || len(pages[1]) != 1 {
			t.Fatalf("pages layout = %v, want oversized group isolated", pageShape(pages))
		}
	})

	t.Run("caps pages and reports omitted entries", func(t *testing.T) {
		t.Parallel()
		groups := make([]dayGroup, 28)
		for i := range groups {
			groups[i] = dayGroup{day: i + 1, entries: make([]domain.CalendarEntry, 8)}
		}
		pages, omitted := paginateDayGroups(&m, groups)
		if len(pages) != calendarMaxPages {
			t.Fatalf("len(pages) = %d, want %d", len(pages), calendarMaxPages)
		}
		rendered := 0
		for _, page := range pages {
			for _, g := range page {
				rendered += len(g.entries)
			}
		}
		if rendered+omitted != 28*8 {
			t.Fatalf("rendered %d + omitted %d != total %d", rendered, omitted, 28*8)
		}
		if omitted == 0 {
			t.Fatal("omitted = 0, want > 0 for capped pagination")
		}
	})
}

func pageShape(pages [][]dayGroup) []int {
	shape := make([]int, len(pages))
	for i, page := range pages {
		shape[i] = len(page)
	}
	return shape
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

	m := newCalendarMetrics()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := entryDisplayName(&m, tt.member); got != tt.want {
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

type pngPhotoServer struct {
	URL   string
	close func()
}

func (s *pngPhotoServer) Close() {
	if s.close != nil {
		s.close()
	}
}

func newPNGServer(t *testing.T, requests *atomic.Int32) *pngPhotoServer {
	return newDelayedPNGServer(t, requests, 0)
}

func newDelayedPNGServer(t *testing.T, requests *atomic.Int32, delay time.Duration) *pngPhotoServer {
	t.Helper()

	pngData := tinyPNG(t)
	previous := photoClient
	photoClient = newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if delay > 0 {
			time.Sleep(delay)
		}
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	}))
	return &pngPhotoServer{
		URL: "https://yt3.googleusercontent.com",
		close: func() {
			photoClient = previous
		},
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return buf.Bytes()
}

func largePNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			offset := img.PixOffset(x, y)
			img.Pix[offset] = uint8(x)
			img.Pix[offset+1] = uint8(y)
			img.Pix[offset+2] = uint8FromClampedInt((x ^ y) & 0xff)
			img.Pix[offset+3] = 255
		}
	}

	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return buf.Bytes()
}

func detailedAvatarSource() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			base := uint8(96 + (x+y)%72)
			if (x/10+y/14)%2 == 0 {
				base = uint8(min(255, int(base)+80))
			}
			if x > 156 && x < 196 && y > 150 && y < 196 {
				base = 20
			}
			if x > 316 && x < 356 && y > 150 && y < 196 {
				base = 20
			}
			if y > 315 && y < 330 && x > 190 && x < 322 {
				base = 240
			}
			img.SetRGBA(x, y, imageColor(base, uint8(max(0, int(base)-26)), uint8(min(255, int(base)+18))))
		}
	}
	return img
}

func imageColor(r, g, b uint8) color.RGBA {
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func drawLegacyBilinearCircularImage(dst *image.RGBA, src image.Image, cx, cy, r int, bgCol color.RGBA) {
	bounds := src.Bounds()
	srcW := float64(bounds.Dx())
	srcH := float64(bounds.Dy())
	diameter := float64(r * 2)
	fr := float64(r)

	for dy := -r; dy < r; dy++ {
		for dx := -r; dx < r; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist > fr+0.5 {
				continue
			}
			sfx := (float64(dx+r) + 0.5) * srcW / diameter
			sfy := (float64(dy+r) + 0.5) * srcH / diameter
			c := legacyBilinearSample(src, bounds, sfx, sfy)
			c = applyEdgeBlend(c, bgCol, dist, fr)
			dst.Set(cx+dx, cy+dy, c)
		}
	}
}

func legacyBilinearSample(src image.Image, bounds image.Rectangle, fx, fy float64) color.RGBA {
	x0 := clampI(int(math.Floor(fx)), bounds.Dx()-1) + bounds.Min.X
	y0 := clampI(int(math.Floor(fy)), bounds.Dy()-1) + bounds.Min.Y
	x1 := clampI(int(math.Floor(fx))+1, bounds.Dx()-1) + bounds.Min.X
	y1 := clampI(int(math.Floor(fy))+1, bounds.Dy()-1) + bounds.Min.Y
	xf := fx - math.Floor(fx)
	yf := fy - math.Floor(fy)

	r00, g00, b00 := legacySampleRGB(src, x0, y0)
	r10, g10, b10 := legacySampleRGB(src, x1, y0)
	r01, g01, b01 := legacySampleRGB(src, x0, y1)
	r11, g11, b11 := legacySampleRGB(src, x1, y1)

	lerp := func(a, b, c, d uint32) uint8 {
		top := float64(a)*(1-xf) + float64(b)*xf
		bot := float64(c)*(1-xf) + float64(d)*xf
		return uint8((top*(1-yf) + bot*yf) / 256)
	}
	return color.RGBA{R: lerp(r00, r10, r01, r11), G: lerp(g00, g10, g01, g11), B: lerp(b00, b10, b01, b11), A: 255}
}

func legacySampleRGB(src image.Image, x, y int) (red, green, blue uint32) {
	c := src.At(x, y)
	if c == nil {
		return 0, 0, 0
	}
	r, g, b, _ := c.RGBA()
	return r, g, b
}

func avatarEdgeEnergy(img *image.RGBA) float64 {
	bounds := img.Bounds()
	var total float64
	var count int
	for y := bounds.Min.Y + 1; y < bounds.Max.Y-1; y++ {
		for x := bounds.Min.X + 1; x < bounds.Max.X-1; x++ {
			c := img.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			right := img.RGBAAt(x+1, y)
			down := img.RGBAAt(x, y+1)
			total += luminanceDelta(c, right) + luminanceDelta(c, down)
			count += 2
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func luminanceDelta(a, b color.RGBA) float64 {
	la := 0.2126*float64(a.R) + 0.7152*float64(a.G) + 0.0722*float64(a.B)
	lb := 0.2126*float64(b.R) + 0.7152*float64(b.G) + 0.0722*float64(b.B)
	return math.Abs(la - lb)
}
