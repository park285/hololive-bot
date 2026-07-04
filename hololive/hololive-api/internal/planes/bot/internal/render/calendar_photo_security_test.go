package render

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type calendarPhotoRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn calendarPhotoRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFetchMemberPhotoBlocksHTTPLoopback(t *testing.T) {
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	photoURL := "http://127.0.0.1/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("blocked photo URL was stored")
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("photo URL was fetched %d times, want 0", got)
	}
}

func TestFetchImageRejectsNilHTTPResponse(t *testing.T) {
	client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))
	withCalendarPhotoClient(t, client)

	img, err := fetchImageWithContext(context.Background(), "https://yt3.googleusercontent.com/avatar=s88-c")
	if err == nil {
		t.Fatal("fetchImageWithContext() error = nil, want nil response error")
	}
	if img != nil {
		t.Fatalf("fetchImageWithContext() image = %#v, want nil", img)
	}
}

func TestFetchMemberPhotoBlocksUnsafeURLsBeforeRoundTrip(t *testing.T) {
	pngData := tinyPNG(t)
	tests := []struct {
		name     string
		photoURL string
	}{
		{
			name:     "allowlisted non-443 port",
			photoURL: "https://yt3.googleusercontent.com:444/avatar=s88-c",
		},
		{
			name:     "link local metadata",
			photoURL: "https://169.254.169.254/avatar=s88-c",
		},
		{
			name:     "rfc1918 private",
			photoURL: "https://10.0.0.1/avatar=s88-c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &calledRoundTripper{body: pngData, contentType: "image/png"}
			withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

			photos := make(map[string]image.Image)
			fetchMemberPhoto(domain.CalendarEntry{
				Member: &domain.Member{Photo: tt.photoURL},
			}, photos)

			if _, ok := photos[tt.photoURL]; ok {
				t.Fatal("blocked photo URL was stored")
			}
			if got := recorder.requests.Load(); got != 0 {
				t.Fatalf("photo URL was fetched %d times, want 0", got)
			}
		})
	}
}

func TestCalendarCardRendererRenderCalendarImageDoesNotDiskCacheBlockedPhotoFallback(t *testing.T) {
	dir := t.TempDir()
	blockedPhotoURL := "https://127.0.0.1/avatar=s88-c"
	entries := []domain.CalendarEntry{
		{
			Kind: domain.CelebrationKindBirthday,
			Member: &domain.Member{
				ShortKoreanName: "페코라",
				Photo:           blockedPhotoURL,
			},
			Day: 15,
		},
	}
	fallbackEntries := []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{ShortKoreanName: "페코라"},
			Day:    15,
		},
	}
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	r := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir))
	pages, err := r.RenderCalendarImages(6, 2026, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImages() error = %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	assertValidPNG(t, pages[0])
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("blocked photo URL was fetched %d times, want 0", got)
	}

	fallbackPages, err := NewCalendarCardRenderer().RenderCalendarImages(6, 2026, fallbackEntries)
	if err != nil {
		t.Fatalf("fallback RenderCalendarImages() error = %v", err)
	}
	if len(fallbackPages) != 1 || !bytes.Equal(pages[0], fallbackPages[0]) {
		t.Fatal("blocked photo render should match default-avatar fallback")
	}

	cacheKey := newCalendarCacheKey(6, 2026, entries)
	if _, ok := r.diskCachedImages(cacheKey); ok {
		t.Fatal("blocked photo fallback was stored in disk cache")
	}
	if entries, readErr := os.ReadDir(filepath.Join(dir, calendarDiskCacheVersion)); readErr != nil {
		if !errors.Is(readErr, os.ErrNotExist) {
			t.Fatalf("read disk cache dir: %v", readErr)
		}
	} else if len(entries) != 0 {
		t.Fatalf("disk cache entries = %d, want 0", len(entries))
	}
	if _, ok := NewCalendarCardRenderer(WithCalendarDiskCacheDir(dir)).diskCachedImages(cacheKey); ok {
		t.Fatal("blocked photo fallback was served from disk cache")
	}
}

func TestFetchMemberPhotoBlocksRedirectToPrivateHost(t *testing.T) {
	pngData := tinyPNG(t)
	var requests atomic.Int32
	client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if req.URL.Hostname() == "yt3.googleusercontent.com" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://127.0.0.1/private=s88-c"}},
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Request:    req,
			}, nil
		}
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	}))
	withCalendarPhotoClient(t, client)

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("redirected private photo URL was stored")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("redirect target requests = %d, want only the initial request", got)
	}
}

func TestFetchMemberPhotoBlocksRedirectWithUserinfo(t *testing.T) {
	pngData := tinyPNG(t)
	var requests atomic.Int32
	client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if req.URL.User == nil {
			return calendarPhotoRedirectResponse(req, "https://user:pass@yt3.googleusercontent.com/private=s88-c"), nil
		}
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	}))
	withCalendarPhotoClient(t, client)

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("redirect to userinfo url was stored")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("redirect target requests = %d, want only the initial request", got)
	}
}

func TestFetchMemberPhotoBlocksRedirectToNon443Port(t *testing.T) {
	pngData := tinyPNG(t)
	var requests atomic.Int32
	client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if req.URL.Hostname() == "yt3.googleusercontent.com" && req.URL.Port() == "" {
			return calendarPhotoRedirectResponse(req, "https://yt3.googleusercontent.com:444/private=s88-c"), nil
		}
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	}))
	withCalendarPhotoClient(t, client)

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("redirected non-443 photo URL was stored")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("redirect target requests = %d, want only the initial request", got)
	}
}

func TestFetchMemberPhotoBlocksThirdRedirect(t *testing.T) {
	pngData := tinyPNG(t)
	var requests atomic.Int32
	withCalendarPhotoResolver(t, &fakeCalendarPhotoResolver{
		addrs: []net.IP{net.ParseIP("93.184.216.34")},
	})
	client := newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		switch req.URL.Path {
		case "/avatar=s1024-c-k-c0x00ffffff-no-rj":
			return calendarPhotoRedirectResponse(req, "/one"), nil
		case "/one":
			return calendarPhotoRedirectResponse(req, "/two"), nil
		case "/two":
			return calendarPhotoRedirectResponse(req, "/three"), nil
		default:
			return calendarPhotoTestResponse(req, "image/png", pngData), nil
		}
	}))
	withCalendarPhotoClient(t, client)

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("photo behind third redirect was stored")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("redirect requests = %d, want 3", got)
	}
}

func TestFetchMemberPhotoBlocksAllowlistedHostResolvingToBlockedIPs(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{name: "loopback", addr: "127.0.0.1"},
		{name: "ipv4 mapped loopback", addr: "::ffff:127.0.0.1"},
		{name: "ipv6 ula", addr: "fd00::1"},
		{name: "ipv6 link local", addr: "fe80::1"},
		{name: "multicast", addr: "ff02::1"},
		{name: "ipv4 unspecified", addr: "0.0.0.0"},
		{name: "ipv6 unspecified", addr: "::"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeCalendarPhotoResolver{
				addrs: []net.IP{net.ParseIP(tt.addr)},
			}
			dialer := &recordingCalendarPhotoDialer{}
			withCalendarPhotoResolver(t, resolver)
			withCalendarPhotoDialer(t, dialer)
			withCalendarPhotoClient(t, newCalendarPhotoHTTPClient())

			photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
			photos := make(map[string]image.Image)
			fetchMemberPhoto(domain.CalendarEntry{
				Member: &domain.Member{Photo: photoURL},
			}, photos)

			if _, ok := photos[photoURL]; ok {
				t.Fatal("allowlisted host resolving to blocked IP was stored")
			}
			if got := resolver.requests.Load(); got != 1 {
				t.Fatalf("resolver requests = %d, want 1", got)
			}
			if got := dialer.requests.Load(); got != 0 {
				t.Fatalf("dialer requests = %d, want 0", got)
			}
		})
	}
}

func TestFetchMemberPhotoBlocksWrongContentType(t *testing.T) {
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "text/plain"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("wrong content type photo was stored")
	}
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("photo requests = %d, want 1", got)
	}
}

func TestFetchMemberPhotoBlocksOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, calendarPhotoMaxBytes+1)
	recorder := &calledRoundTripper{body: body, contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; ok {
		t.Fatal("oversized photo body was stored")
	}
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("photo requests = %d, want 1", got)
	}
}

func TestFetchMemberPhotoAcceptsAllowlistedHTTPSPNG(t *testing.T) {
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	photos := make(map[string]image.Image)
	fetchMemberPhoto(domain.CalendarEntry{
		Member: &domain.Member{Photo: photoURL},
	}, photos)

	if _, ok := photos[photoURL]; !ok {
		t.Fatal("allowlisted HTTPS PNG photo was not stored")
	}
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("photo requests = %d, want 1", got)
	}
}

type calledRoundTripper struct {
	requests    atomic.Int32
	body        []byte
	contentType string
}

func (rt *calledRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests.Add(1)
	return calendarPhotoTestResponse(req, rt.contentType, rt.body), nil
}

func calendarPhotoTestResponse(req *http.Request, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func calendarPhotoRedirectResponse(req *http.Request, location string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{location}},
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Request:    req,
	}
}

func newCalendarPhotoTestClient(rt http.RoundTripper) *http.Client {
	client := *photoClient
	client.Transport = rt
	return &client
}

func withCalendarPhotoClient(t *testing.T, client *http.Client) {
	t.Helper()

	previous := photoClient
	photoClient = client
	t.Cleanup(func() {
		photoClient = previous
	})
}

type fakeCalendarPhotoResolver struct {
	requests atomic.Int32
	addrs    []net.IP
	err      error
}

func (r *fakeCalendarPhotoResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	r.requests.Add(1)
	if r.err != nil {
		return nil, r.err
	}
	return r.addrs, nil
}

type recordingCalendarPhotoDialer struct {
	requests atomic.Int32
}

func (d *recordingCalendarPhotoDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.requests.Add(1)
	return nil, errors.New("unexpected calendar photo dial")
}

func withCalendarPhotoResolver(t *testing.T, resolver calendarPhotoResolver) {
	t.Helper()

	previous := calendarPhotoDNSResolver
	calendarPhotoDNSResolver = resolver
	t.Cleanup(func() {
		calendarPhotoDNSResolver = previous
	})
}

func withCalendarPhotoDialer(t *testing.T, dialer calendarPhotoDialer) {
	t.Helper()

	previous := calendarPhotoNetworkDialer
	calendarPhotoNetworkDialer = dialer
	t.Cleanup(func() {
		calendarPhotoNetworkDialer = previous
	})
}
