package render

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
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
	if bounds.Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", bounds.Dx(), calendarOutputWidth)
	}
	outputHeaderH := newCalendarMetrics(1).headerH * calendarOutputWidth / canvasWidth
	if bounds.Dy() <= outputHeaderH {
		t.Error("height should be larger than header for entries")
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_UsesTransportFriendlyCanvas(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	data, err := r.RenderCalendarImage(6, 2026, []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
	})
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	img, decErr := png.Decode(bytes.NewReader(data))
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
	r := newCalendarMetrics(1).avatarSize / 2
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

	first, err := r.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImage() error = %v", err)
	}
	first[0] = 0

	second, err := r.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImage() error = %v", err)
	}

	assertValidPNG(t, second)
	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestCalendarCardRenderer_RenderCalendarImage_CoalescesConcurrentMonthlyCacheMisses(t *testing.T) {
	var requests atomic.Int32
	pngData := tinyPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
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
			data, err := r.RenderCalendarImage(6, 2026, entries)
			if err != nil {
				errs <- err
				return
			}
			if !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
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

	if _, err := r.RenderCalendarImage(6, 2026, entries); err != nil {
		t.Fatalf("first RenderCalendarImage() error = %v", err)
	}
	if _, err := r.RenderCalendarImage(6, 2026, changedEntries); err != nil {
		t.Fatalf("changed RenderCalendarImage() error = %v", err)
	}

	if got, want := requests.Load(), int32(2); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestFetchMemberPhotoRequestsHighResolutionThumbnail(t *testing.T) {
	var requestedPath string
	pngData := tinyPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	photos := make(map[string]image.Image)
	photoURL := server.URL + "/avatar=s88-c"
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	img, err := fetchImage(server.URL)
	if err != nil {
		t.Fatalf("fetchImage() error = %v", err)
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

func newPNGServer(t *testing.T, requests *atomic.Int32) *httptest.Server {
	t.Helper()

	pngData := tinyPNG(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
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
			img.Pix[offset+2] = uint8(x ^ y)
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
	x0 := clampI(int(math.Floor(fx)), 0, bounds.Dx()-1) + bounds.Min.X
	y0 := clampI(int(math.Floor(fy)), 0, bounds.Dy()-1) + bounds.Min.Y
	x1 := clampI(int(math.Floor(fx))+1, 0, bounds.Dx()-1) + bounds.Min.X
	y1 := clampI(int(math.Floor(fy))+1, 0, bounds.Dy()-1) + bounds.Min.Y
	xf := fx - math.Floor(fx)
	yf := fy - math.Floor(fy)

	r00, g00, b00, _ := src.At(x0, y0).RGBA()
	r10, g10, b10, _ := src.At(x1, y0).RGBA()
	r01, g01, b01, _ := src.At(x0, y1).RGBA()
	r11, g11, b11, _ := src.At(x1, y1).RGBA()

	lerp := func(a, b, c, d uint32) uint8 {
		top := float64(a)*(1-xf) + float64(b)*xf
		bot := float64(c)*(1-xf) + float64(d)*xf
		return uint8((top*(1-yf) + bot*yf) / 256)
	}
	return color.RGBA{R: lerp(r00, r10, r01, r11), G: lerp(g00, g10, g01, g11), B: lerp(b00, b10, b01, b11), A: 255}
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
