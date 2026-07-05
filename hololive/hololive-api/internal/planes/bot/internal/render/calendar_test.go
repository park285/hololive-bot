package render

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/http"
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
	first, err := firstRenderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, first)

	secondRenderer := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	second, err := secondRenderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, second)

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

	r.storeDiskCachedImage(old, pngBytes)
	r.storeDiskCachedImage(otherMonth, pngBytes)
	r.storeDiskCachedImage(fresh, pngBytes)

	if _, ok := r.diskCachedImage(old); ok {
		t.Fatal("stale same-month hash should be pruned")
	}
	if data, ok := r.diskCachedImage(fresh); !ok || !bytes.Equal(data, pngBytes) {
		t.Fatalf("latest same-month hash should remain, got ok=%v", ok)
	}
	if _, ok := r.diskCachedImage(otherMonth); !ok {
		t.Fatal("other-month entry must not be pruned")
	}
}

func TestDiskCache_ConcurrentCrossHashStoreServesValidDataOrMiss(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer(WithCalendarDiskCacheDir(t.TempDir()))
	pngBytes := tinyPNG(t)

	keys := []calendarCacheKey{
		{year: 2026, month: 6, entriesHash: strings.Repeat("a", 64)},
		{year: 2026, month: 6, entriesHash: strings.Repeat("b", 64)},
	}

	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Go(func() {
			for range 40 {
				r.storeDiskCachedImage(key, pngBytes)
				got, ok := r.diskCachedImage(key)
				if ok && !bytes.Equal(got, pngBytes) {
					t.Errorf("corrupt data for hash %s", key.entriesHash[:4])
					return
				}
			}
		})
	}
	wg.Wait()
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

	data, err := r.RenderCalendarImage(1, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	assertValidPNG(t, data)
}

func TestCalendarCardRenderer_CompactsBusyMonthIntoSingleImage(t *testing.T) {
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

	data, err := r.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImage() error = %v", err)
	}

	img, decErr := png.Decode(bytes.NewReader(data))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}
	if img.Bounds().Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", img.Bounds().Dx(), calendarOutputWidth)
	}
	if img.Bounds().Dy() > calendarOutputHeight {
		t.Errorf("height %d exceeds single-image budget %d", img.Bounds().Dy(), calendarOutputHeight)
	}
}

func TestCalendarCompactRatio_NaturalFitKeepsFullScale(t *testing.T) {
	t.Parallel()

	groups := []dayGroup{{day: 1, entries: []domain.CalendarEntry{{Day: 1}}}}
	if got := calendarCompactRatio(groups); got != 1 {
		t.Fatalf("calendarCompactRatio() = %v, want 1", got)
	}
}

func TestCalendarCompactRatio_ShrinksOverflowingMonthToTarget(t *testing.T) {
	t.Parallel()

	groups := make([]dayGroup, 28)
	for i := range groups {
		groups[i] = dayGroup{day: i + 1, entries: make([]domain.CalendarEntry, 8)}
	}

	ratio := calendarCompactRatio(groups)
	if ratio <= 0 || ratio >= 1 {
		t.Fatalf("calendarCompactRatio() = %v, want (0, 1)", ratio)
	}

	m := newCalendarMetrics(ratio)
	if h := calculateCanvasHeight(&m, groups); h > calendarTargetInnerH {
		t.Fatalf("compacted height %d exceeds target %d", h, calendarTargetInnerH)
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

	m := newCalendarMetrics(1)
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
			img.Pix[offset+2] = uint8((x ^ y) & 0xff)
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
