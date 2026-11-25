package render

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testShortKoreanName = "페코라"
	testAvatarURL       = "https://yt3.googleusercontent.com/avatar=s88-c"
)

func TestCalendarCardRenderer_RenderCalendarImages_ReusesMonthlyCache(t *testing.T) {
	var requests atomic.Int32

	server := newPNGServer(t, &requests)

	defer server.Close()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: testShortKoreanName, Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	first, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(first) != 1 || len(first[0]) == 0 {
		t.Fatal("first RenderCalendarImagesWithTheme() returned empty image data")
	}

	first[0][0] = 0

	second, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImagesWithTheme() error = %v", err)
	}

	assertValidPNG(t, second[0])

	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestAwaitCalendarRenderCallRejectsMissingImageData(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	close(done)

	call := &calendarRenderCall{done: done}

	pages, err := NewCalendarCardRenderer().awaitCalendarRenderCall(t.Context(), calendarCacheKey{}, call)
	if err == nil {
		t.Fatal("awaitCalendarRenderCall() error = nil, want missing image data error")
	}

	if pages != nil {
		t.Fatalf("awaitCalendarRenderCall() pages = %d, want nil", len(pages))
	}
}

func TestCalendarCardRenderer_CoalescesConcurrentColdRenders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		pngData := tinyPNG(t)

		var requests atomic.Int32

		withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			<-release

			return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
		})))

		renderer := NewCalendarCardRenderer()
		entries := []domain.CalendarEntry{{
			Kind: domain.CelebrationKindBirthday,
			Member: &domain.Member{
				ShortKoreanName: testShortKoreanName,
				Photo:           testAvatarURL,
			},
			Day: 15,
		}}

		start := make(chan struct{})
		errCh := make(chan error, 12)

		var wg sync.WaitGroup

		for range 12 {
			wg.Go(func() {
				<-start

				pages, err := renderer.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
				if err == nil && (len(pages) != 1 || !isPNGData(pages[0])) {
					err = errors.New("render returned invalid PNG")
				}

				errCh <- err
			})
		}

		close(start)
		synctest.Wait()
		close(release)
		wg.Wait()
		close(errCh)

		for err := range errCh {
			if err != nil {
				t.Fatal(err)
			}
		}

		if got := requests.Load(); got != 1 {
			t.Fatalf("photo requests = %d, want 1 coalesced cold render", got)
		}
	})
}

func TestCalendarCardRenderer_RenderCalendarImages_ReusesSharedCacheAcrossRenderers(t *testing.T) {
	var requests atomic.Int32

	server := newPNGServer(t, &requests)

	defer server.Close()

	store := newTestCalendarImageCache(t)
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: testShortKoreanName, Photo: server.URL + "/avatar=s88-c"},
			Day:    15,
		},
	}

	firstRenderer := NewCalendarCardRenderer(WithCalendarImageCache(store))

	first, err := firstRenderer.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImagesWithTheme() error = %v", err)
	}

	assertValidPNG(t, first[0])

	secondRenderer := NewCalendarCardRenderer(WithCalendarImageCache(store))

	second, err := secondRenderer.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImagesWithTheme() error = %v", err)
	}

	assertValidPNG(t, second[0])

	if got, want := requests.Load(), int32(1); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}
}

func TestSharedCachedAlbum_ReplacesStaleMonthHash(t *testing.T) {
	store, mini := newTestCalendarImageCacheWithMini(t)
	r := NewCalendarCardRenderer(WithCalendarImageCache(store))
	pages := [][]byte{tinyPNG(t)}

	old := calendarCacheKey{year: 2026, month: 6, theme: CalendarThemePrismDaylight, entriesHash: strings.Repeat("a", 64)}
	fresh := calendarCacheKey{year: 2026, month: 6, theme: CalendarThemePrismDaylight, entriesHash: strings.Repeat("b", 64)}
	otherMonth := calendarCacheKey{year: 2026, month: 7, theme: CalendarThemePrismDaylight, entriesHash: strings.Repeat("c", 64)}

	r.storeSharedCachedAlbum(t.Context(), old, pages)
	r.storeSharedCachedAlbum(t.Context(), otherMonth, pages)
	r.storeSharedCachedAlbum(t.Context(), fresh, pages)

	if _, _, ok := r.sharedCachedAlbum(t.Context(), old); ok {
		t.Fatal("stale same-month hash should be replaced")
	}

	if got, _, ok := r.sharedCachedAlbum(t.Context(), fresh); !ok || !slices.EqualFunc(got, pages, bytes.Equal) {
		t.Fatalf("latest same-month hash should remain, got ok=%v", ok)
	}

	if _, _, ok := r.sharedCachedAlbum(t.Context(), otherMonth); !ok {
		t.Fatal("other-month entry must not be replaced")
	}

	if ttl := mini.TTL(calendarAlbumCacheKey(fresh)); ttl != calendarImageCacheTTL {
		t.Fatalf("album cache TTL = %s, want %s", ttl, calendarImageCacheTTL)
	}

	mini.SetError("valkey unavailable")

	if _, _, ok := r.sharedCachedAlbum(t.Context(), fresh); ok {
		t.Fatal("shared cache error should be a miss")
	}
}

func TestSharedCache_ConcurrentCrossHashStoreServesValidDataOrMiss(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer(WithCalendarImageCache(newTestCalendarImageCache(t)))
	pages := [][]byte{tinyPNG(t)}

	keys := []calendarCacheKey{
		{year: 2026, month: 6, theme: CalendarThemePrismDaylight, entriesHash: strings.Repeat("a", 64)},
		{year: 2026, month: 6, theme: CalendarThemePrismDaylight, entriesHash: strings.Repeat("b", 64)},
	}

	var wg sync.WaitGroup

	for _, key := range keys {
		wg.Go(func() {
			for range 40 {
				r.storeSharedCachedAlbum(t.Context(), key, pages)

				got, _, ok := r.sharedCachedAlbum(t.Context(), key)
				if ok && !slices.EqualFunc(got, pages, bytes.Equal) {
					t.Errorf("corrupt data for hash %s", key.entriesHash[:4])

					return
				}
			}
		})
	}

	wg.Wait()
}

func TestCalendarCardRenderer_RenderCalendarImages_RefreshesCacheWhenEntriesChange(t *testing.T) {
	var requests atomic.Int32

	server := newPNGServer(t, &requests)

	defer server.Close()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: testShortKoreanName, Photo: server.URL + "/avatar=s88-c"},
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

	if _, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries); err != nil {
		t.Fatalf("first RenderCalendarImagesWithTheme() error = %v", err)
	}

	if _, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, changedEntries); err != nil {
		t.Fatalf("changed RenderCalendarImagesWithTheme() error = %v", err)
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

		return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
	})))

	photoURL := testAvatarURL
	_, stored := fetchMemberPhoto(t.Context(), photoURL)

	if !stored {
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
		return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
	})))

	img, err := fetchImageWithContext(t.Context(), "https://yt3.googleusercontent.com/avatar=s1024-c")
	if err != nil {
		t.Fatalf("fetchImageWithContext() error = %v", err)
	}

	if img == nil {
		t.Fatal("fetchImageWithContext() returned nil image")
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

	leftKey := newCalendarCacheKey(6, 2026, CalendarThemePrismDaylight, left)
	rightKey := newCalendarCacheKey(6, 2026, CalendarThemePrismDaylight, right)
	otherMonthKey := newCalendarCacheKey(7, 2026, CalendarThemePrismDaylight, left)
	otherYearKey := newCalendarCacheKey(6, 2027, CalendarThemePrismDaylight, left)
	otherThemeKey := newCalendarCacheKey(6, 2026, CalendarThemeKurokamiRisograph, left)

	if leftKey == rightKey || leftKey == otherMonthKey || leftKey == otherYearKey || leftKey == otherThemeKey {
		t.Fatal("cache keys should differ for delimiter-containing fields, month, year, and theme")
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
		{"short korean name", &domain.Member{ShortKoreanName: testShortKoreanName, NameKo: "우사다 페코라", Name: "Pekora"}, testShortKoreanName},
		{"korean name fallback", &domain.Member{NameKo: "우사다 페코라", Name: "Pekora"}, "우사다 페코라"},
		{"english name fallback", &domain.Member{Name: "Pekora"}, "Pekora"},
	}

	m := newCalendarMetrics(1)

	m.strings = loadSeededCalendarStore(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := entryDisplayName(t.Context(), &m, tt.member); got != tt.want {
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
	URL           string
	restoreClient func()
}

func (s *pngPhotoServer) Close() {
	if s.restoreClient != nil {
		s.restoreClient()
	}
}

func newPNGServer(t *testing.T, requests *atomic.Int32) *pngPhotoServer {
	t.Helper()

	pngData := tinyPNG(t)
	previous := photoClient

	photoClient = newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)

		return calendarPhotoTestResponse(req, calendarPhotoContentTypePNG, pngData), nil
	}))

	return &pngPhotoServer{
		URL: "https://yt3.googleusercontent.com",
		restoreClient: func() {
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

func newTestCalendarImageCache(t *testing.T) sharedcache.Store {
	t.Helper()

	store, _ := newTestCalendarImageCacheWithMini(t)

	return store
}

func newTestCalendarImageCacheWithMini(t *testing.T) (sharedcache.Store, *miniredis.Miniredis) {
	t.Helper()

	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	store := sharedcache.NewValkeyStore(client)
	if store == nil {
		t.Fatal("test Valkey store is nil")
	}

	return store, mini
}
