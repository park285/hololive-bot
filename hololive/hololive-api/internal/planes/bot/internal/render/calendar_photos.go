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
	calendarPhotoMaxDimension  = 4096
	calendarPhotoMaxPixels     = 8 << 20
)

type calendarPhotoFetchState struct {
	attempted   map[string]struct{}
	fetches     int
	cachePolicy calendarCachePolicy
}

type calendarPhotoFetchResult struct {
	photos      map[string]image.Image
	cachePolicy calendarCachePolicy
}

func fetchMemberPhotos(parent context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
	if parent == nil {
		return calendarPhotoFetchResult{}, errors.New("calendar photo context is nil")
	}
	budgetCtx, cancel := context.WithTimeout(parent, photoFetchBudget)
	defer cancel()
	return fetchMemberPhotosWithinBudget(parent, budgetCtx, entries)
}

func fetchMemberPhotosWithinBudget(parent, budgetCtx context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
	result := calendarPhotoFetchResult{photos: make(map[string]image.Image)}
	state := newCalendarPhotoFetchState()
	for _, e := range entries {
		if err := parent.Err(); err != nil {
			return result, err
		}
		if budgetCtx.Err() != nil {
			state.markUncacheable()
			break
		}
		if state.wouldTruncate(e, result.photos) {
			state.markUncacheable()
			break
		}
		state.fetch(budgetCtx, e, result.photos)
	}
	if err := parent.Err(); err != nil {
		return result, err
	}
	result.cachePolicy = state.cachePolicy
	return result, nil
}

func newCalendarPhotoFetchState() *calendarPhotoFetchState {
	return &calendarPhotoFetchState{
		attempted: make(map[string]struct{}),
		cachePolicy: calendarCachePolicy{
			memoryCacheable: true,
			diskCacheable:   true,
		},
	}
}

func (s *calendarPhotoFetchState) wouldTruncate(e domain.CalendarEntry, photos map[string]image.Image) bool {
	return s.needsFetch(e, photos) && s.fetches >= calendarPhotoMaxFetches
}

func (s *calendarPhotoFetchState) needsFetch(e domain.CalendarEntry, photos map[string]image.Image) bool {
	photoURL, ok := calendarEntryPhotoURL(e)
	return ok && !s.alreadyFetchedOrAttempted(photoURL, photos)
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
		if ctx.Err() != nil {
			s.markUncacheable()
		}
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
	s.cachePolicy.diskCacheable = false
}

func (s *calendarPhotoFetchState) markUncacheable() {
	s.cachePolicy.memoryCacheable = false
	s.cachePolicy.diskCacheable = false
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
	config, err := decodeCalendarPhotoConfig(data, contentType)
	if err != nil {
		return nil, err
	}
	if err := validateCalendarPhotoConfig(config); err != nil {
		return nil, err
	}

	return decodeCalendarPhotoPixels(data, contentType)
}

func decodeCalendarPhotoPixels(data []byte, contentType string) (image.Image, error) {
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

func decodeCalendarPhotoConfig(data []byte, contentType string) (image.Config, error) {
	reader := bytes.NewReader(data)
	switch contentType {
	case "image/png":
		return png.DecodeConfig(reader)
	case "image/jpeg":
		return jpeg.DecodeConfig(reader)
	case "image/webp":
		return webp.DecodeConfig(reader)
	default:
		return image.Config{}, fmt.Errorf("unsupported image format")
	}
}

func validateCalendarPhotoConfig(config image.Config) error {
	if config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("calendar photo has invalid dimensions %dx%d", config.Width, config.Height)
	}
	if config.Width > calendarPhotoMaxDimension || config.Height > calendarPhotoMaxDimension {
		return fmt.Errorf("calendar photo dimensions %dx%d exceed %d", config.Width, config.Height, calendarPhotoMaxDimension)
	}
	pixels := uint64(config.Width) * uint64(config.Height)
	if pixels > uint64(calendarPhotoMaxPixels) {
		return fmt.Errorf("calendar photo pixel count %d exceeds %d", pixels, calendarPhotoMaxPixels)
	}
	return nil
}

func thumbnailURL(original string, size int) string {
	if before, _, ok := strings.Cut(original, "=s"); ok {
		return fmt.Sprintf("%s=s%d-c-k-c0x00ffffff-no-rj", before, size)
	}
	return original
}
