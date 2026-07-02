package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"golang.org/x/image/webp"
)

var photoClient = newCalendarPhotoHTTPClient()

const (
	photoFetchBudget           = 15 * time.Second
	calendarPhotoMaxFetches    = 24
	calendarPhotoThumbnailSize = 1024
	calendarPhotoMaxBytes      = 2 << 20
)

type calendarPhotoFetchState struct {
	attempted     map[string]struct{}
	fetches       int
	diskCacheable bool
}

func fetchMemberPhotos(entries []domain.CalendarEntry) (map[string]image.Image, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), photoFetchBudget)
	defer cancel()

	photos := make(map[string]image.Image)
	state := newCalendarPhotoFetchState()
	for _, e := range entries {
		if state.shouldStop(ctx) {
			state.markDiskUncacheable()
			break
		}
		state.fetch(ctx, e, photos)
	}
	return photos, state.diskCacheable
}

func newCalendarPhotoFetchState() *calendarPhotoFetchState {
	return &calendarPhotoFetchState{
		attempted:     make(map[string]struct{}),
		diskCacheable: true,
	}
}

func fetchPhotosByURL(urls []string) map[string]image.Image {
	ctx, cancel := context.WithTimeout(context.Background(), photoFetchBudget)
	defer cancel()

	photos := make(map[string]image.Image)
	state := newCalendarPhotoFetchState()
	for _, u := range urls {
		if state.shouldStop(ctx) {
			break
		}
		if u == "" || state.alreadyFetchedOrAttempted(u, photos) {
			continue
		}
		state.attempted[u] = struct{}{}
		state.fetches++
		if img, err := fetchImageWithContext(ctx, thumbnailURL(u, calendarPhotoThumbnailSize)); err == nil {
			photos[u] = img
		}
	}
	return photos
}

func (s *calendarPhotoFetchState) shouldStop(ctx context.Context) bool {
	return ctx.Err() != nil || s.fetches >= calendarPhotoMaxFetches
}

func (s *calendarPhotoFetchState) fetch(ctx context.Context, e domain.CalendarEntry, photos map[string]image.Image) {
	photoURL, ok := calendarEntryPhotoURL(e)
	if !ok || s.alreadyFetchedOrAttempted(photoURL, photos) {
		return
	}
	s.attempted[photoURL] = struct{}{}
	s.fetches++
	if !fetchMemberPhotoWithContext(ctx, e, photos) {
		s.markDiskUncacheable()
	}
}

func calendarEntryPhotoURL(e domain.CalendarEntry) (string, bool) {
	if e.Member == nil || e.Member.Photo == "" {
		return "", false
	}
	return e.Member.Photo, true
}

func (s *calendarPhotoFetchState) alreadyFetchedOrAttempted(photoURL string, photos map[string]image.Image) bool {
	if _, ok := photos[photoURL]; ok {
		return true
	}
	_, ok := s.attempted[photoURL]
	return ok
}

func (s *calendarPhotoFetchState) markDiskUncacheable() {
	s.diskCacheable = false
}

func fetchMemberPhoto(e domain.CalendarEntry, photos map[string]image.Image) {
	fetchMemberPhotoWithContext(context.Background(), e, photos)
}

func fetchMemberPhotoWithContext(ctx context.Context, e domain.CalendarEntry, photos map[string]image.Image) bool {
	if e.Member == nil || e.Member.Photo == "" {
		return true
	}
	if _, ok := photos[e.Member.Photo]; ok {
		return true
	}
	url := thumbnailURL(e.Member.Photo, calendarPhotoThumbnailSize)
	if img, err := fetchImageWithContext(ctx, url); err == nil {
		photos[e.Member.Photo] = img
		return true
	} else {
		slog.Debug("calendar photo fetch skipped",
			slog.String("photo_host", calendarPhotoURLHost(url)),
			slog.String("err", err.Error()),
		)
		return false
	}
}

func fetchImage(url string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(context.Background(), calendarPhotoRequestTimeout)
	defer cancel()
	return fetchImageWithContext(ctx, url)
}

func fetchImageWithContext(ctx context.Context, url string) (image.Image, error) {
	if err := validateCalendarPhotoURL(url); err != nil {
		return nil, err
	}

	resp, err := fetchCalendarPhotoResponse(ctx, url)
	if err != nil {
		return nil, err
	}
	defer closeResponseBody(resp.Body)

	contentType, err := validateCalendarPhotoResponse(resp)
	if err != nil {
		return nil, err
	}

	data, err := readCalendarPhotoData(resp.Body)
	if err != nil {
		return nil, err
	}
	return decodeCalendarPhoto(data, contentType)
}

func fetchCalendarPhotoResponse(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build calendar photo request: %w", err)
	}
	resp, err := photoClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("calendar photo response is nil")
	}
	if resp.Body == nil {
		return nil, errors.New("calendar photo response body is nil")
	}

	return resp, nil
}

func validateCalendarPhotoResponse(resp *http.Response) (string, error) {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("calendar photo status %d is not successful", resp.StatusCode)
	}
	contentType, err := validateCalendarPhotoContentType(resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	return contentType, nil
}

func readCalendarPhotoData(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, calendarPhotoMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > calendarPhotoMaxBytes {
		return nil, errors.New("image exceeds calendar photo byte limit")
	}
	return data, nil
}

func closeResponseBody(body io.Closer) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		slog.Debug("calendar photo response body close failed", slog.Any("error", err))
	}
}

func decodeCalendarPhoto(data []byte, contentType string) (image.Image, error) {
	switch contentType {
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/webp":
		return webp.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported image format")
	}
}

func thumbnailURL(original string, size int) string {
	if before, _, ok := strings.Cut(original, "=s"); ok {
		return fmt.Sprintf("%s=s%d-c-k-c0x00ffffff-no-rj", before, size)
	}
	return original
}
