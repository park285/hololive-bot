package render

import (
	"context"
	"errors"
	"fmt"
	"image"
	"net/http"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestValidateCalendarPhotoConfigRejectsImageBombDimensions(t *testing.T) {
	t.Parallel()

	for _, config := range []image.Config{
		{Width: 0, Height: 1},
		{Width: 1, Height: 0},
		{Width: calendarPhotoMaxDimension + 1, Height: 1},
		{Width: calendarPhotoMaxDimension, Height: 3000},
	} {
		if err := validateCalendarPhotoConfig(config); err == nil {
			t.Fatalf("validateCalendarPhotoConfig(%+v) error = nil", config)
		}
	}

	if err := validateCalendarPhotoConfig(image.Config{Width: 1024, Height: 1024}); err != nil {
		t.Fatalf("valid image config rejected: %v", err)
	}
}

func TestCalendarCardRendererCancelledRenderDoesNotFetchOrPoisonCache(t *testing.T) {
	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	entries := []domain.CalendarEntry{{
		Kind: domain.CelebrationKindBirthday,
		Member: &domain.Member{
			ShortKoreanName: "페코라",
			Photo:           photoURL,
		},
		Day: 15,
	}}
	renderer := NewCalendarCardRenderer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data, err := renderer.RenderCalendarImageContext(ctx, 6, 2026, entries)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled render error = %v, want context.Canceled", err)
	}
	if data != nil {
		t.Fatalf("cancelled render returned %d bytes", len(data))
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("cancelled render fetched %d photos, want 0", got)
	}

	data, err = renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("subsequent render error = %v", err)
	}
	assertValidPNG(t, data)
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("subsequent render fetched %d photos, want 1; cancelled result may have poisoned cache", got)
	}
}

func TestFetchMemberPhotosBudgetExpiryReturnsPartialPhotos(t *testing.T) {
	parent := context.Background()

	pngData := tinyPNG(t)
	requests := 0
	firstPhotoURL := "https://yt3.googleusercontent.com/first=s88-c"
	secondPhotoURL := "https://yt3.googleusercontent.com/second=s88-c"
	entries := []domain.CalendarEntry{
		{Member: &domain.Member{Photo: firstPhotoURL}},
		{Member: &domain.Member{Photo: secondPhotoURL}},
	}

	budgetCtx, cancel := context.WithCancel(parent)
	defer cancel()
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return calendarPhotoTestResponse(req, "image/png", pngData), nil
		}
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})))

	result, err := fetchMemberPhotosWithinBudget(parent, budgetCtx, entries)
	if err != nil {
		t.Fatalf("fetchMemberPhotosWithinBudget() error = %v", err)
	}
	if _, ok := result.photos[firstPhotoURL]; !ok {
		t.Fatal("completed photo was discarded after budget expiry")
	}
	if _, ok := result.photos[secondPhotoURL]; ok {
		t.Fatal("timed-out photo was stored")
	}
	if result.cachePolicy.memoryCacheable {
		t.Fatal("partial photo result should not be memory-cacheable")
	}
	if result.cachePolicy.diskCacheable {
		t.Fatal("partial photo result should not be disk-cacheable")
	}
}

func TestFetchMemberPhotosParentCancellationReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchMemberPhotos(ctx, []domain.CalendarEntry{{
		Member: &domain.Member{Photo: "https://yt3.googleusercontent.com/avatar=s88-c"},
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchMemberPhotos() error = %v, want context.Canceled", err)
	}
}

func TestCalendarCardRendererGlobalPhotoFetchTruncationDoesNotPopulateCaches(t *testing.T) {
	pngData := tinyPNG(t)
	requests := 0
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return calendarPhotoTestResponse(req, "image/png", pngData), nil
	})))

	entries := make([]domain.CalendarEntry, 0, calendarPhotoMaxFetches+1)
	for i := range calendarPhotoMaxFetches + 1 {
		entries = append(entries, domain.CalendarEntry{
			Kind: domain.CelebrationKindBirthday,
			Member: &domain.Member{
				ShortKoreanName: fmt.Sprintf("member-%d", i),
				Photo:           fmt.Sprintf("https://yt3.googleusercontent.com/avatar-%d=s88-c", i),
			},
			Day: 15,
		})
	}

	renderer := NewCalendarCardRenderer(WithCalendarDiskCacheDir(t.TempDir()))
	first, err := renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, first)

	cacheKey := newCalendarCacheKey(6, 2026, entries)
	if _, ok := renderer.cachedImage(cacheKey); ok {
		t.Fatal("globally truncated render was stored in memory cache")
	}
	if _, ok := renderer.diskCachedImage(cacheKey); ok {
		t.Fatal("globally truncated render was stored in disk cache")
	}

	second, err := renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, second)
	if got, want := requests, calendarPhotoMaxFetches*2; got != want {
		t.Fatalf("photo requests = %d, want %d; same-key render did not refetch", got, want)
	}
}

func TestCalendarCardRendererOrdinaryPhotoFetchFailureStillPopulatesMemoryCache(t *testing.T) {
	requests := 0
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(calendarPhotoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return calendarPhotoTestResponse(req, "text/plain", []byte("not an image")), nil
	})))

	entries := []domain.CalendarEntry{{
		Kind: domain.CelebrationKindBirthday,
		Member: &domain.Member{
			ShortKoreanName: "페코라",
			Photo:           "https://yt3.googleusercontent.com/avatar=s88-c",
		},
		Day: 15,
	}}
	renderer := NewCalendarCardRenderer(WithCalendarDiskCacheDir(t.TempDir()))

	first, err := renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("first RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, first)

	cacheKey := newCalendarCacheKey(6, 2026, entries)
	if _, ok := renderer.cachedImage(cacheKey); !ok {
		t.Fatal("ordinary photo fetch failure was not stored in memory cache")
	}
	if _, ok := renderer.diskCachedImage(cacheKey); ok {
		t.Fatal("ordinary photo fetch failure was stored in disk cache")
	}

	second, err := renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("second RenderCalendarImage() error = %v", err)
	}
	assertValidPNG(t, second)
	if got, want := requests, 1; got != want {
		t.Fatalf("photo requests = %d, want %d; ordinary failure should reuse memory cache", got, want)
	}
}
