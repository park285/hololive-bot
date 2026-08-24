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

	"golang.org/x/image/webp"

	"github.com/kapu/hololive-shared/pkg/domain"
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

	out, err := fetchMemberPhotosWithinBudget(parent, budgetCtx, entries)
	if err != nil {
		return out, fmt.Errorf("fetch member photos within budget: %w", err)
	}

	return out, nil
}

func fetchMemberPhotosWithinBudget(parent, budgetCtx context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
	result := calendarPhotoFetchResult{photos: make(map[string]image.Image)}
	state := newCalendarPhotoFetchState()

	for _, e := range entries {
		if err := parent.Err(); err != nil {
			return result, fmt.Errorf("before member photo download: %w", err)
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
		return result, fmt.Errorf("after member photo downloads: %w", err)
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

	img, err := fetchImageWithContext(ctx, url)
	if err != nil {
		slog.Debug("calendar photo fetch skipped",
			slog.String("photo_host", calendarPhotoURLHost(url)),
			slog.String("err", err.Error()),
		)

		return false
	}

	photos[e.Member.Photo] = img

	return true
}

func fetchImage(url string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(context.Background(), calendarPhotoRequestTimeout)
	defer cancel()

	out, err := fetchImageWithContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch image with context: %w", err)
	}

	return out, nil
}

func fetchImageWithContext(ctx context.Context, url string) (image.Image, error) {
	if err := validateCalendarPhotoURL(url); err != nil {
		return nil, fmt.Errorf("validate calendar photo URL: %w", err)
	}

	resp, err := fetchCalendarPhotoResponse(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch calendar photo response: %w", err)
	}
	defer closeResponseBody(resp.Body)

	contentType, err := validateCalendarPhotoResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("validate calendar photo response: %w", err)
	}

	data, err := readCalendarPhotoData(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read calendar photo data: %w", err)
	}

	out, err := decodeCalendarPhoto(data, contentType)
	if err != nil {
		return nil, fmt.Errorf("decode calendar photo: %w", err)
	}

	return out, nil
}

func fetchCalendarPhotoResponse(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build calendar photo request: %w", err)
	}

	resp, err := photoClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
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
		return "", fmt.Errorf("validate calendar photo content type: %w", err)
	}

	return contentType, nil
}

func readCalendarPhotoData(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, calendarPhotoMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read all: %w", err)
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
		return nil, fmt.Errorf("decode calendar photo config: %w", err)
	}

	if validateErr := validateCalendarPhotoConfig(config); validateErr != nil {
		return nil, fmt.Errorf("validate calendar photo config: %w", validateErr)
	}

	out, err := decodeCalendarPhotoPixels(data, contentType)
	if err != nil {
		return nil, fmt.Errorf("decode calendar photo pixels: %w", err)
	}

	return out, nil
}

func decodeCalendarPhotoPixels(data []byte, contentType string) (image.Image, error) {
	decoder := calendarPhotoPixelDecoder(contentType)
	if decoder == nil {
		return nil, errors.New("unsupported image format")
	}

	out, err := decoder(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return out, nil
}

func calendarPhotoPixelDecoder(contentType string) func(io.Reader) (image.Image, error) {
	switch contentType {
	case calendarPhotoContentTypePNG:
		return png.Decode
	case calendarPhotoContentTypeJPEG:
		return jpeg.Decode
	case calendarPhotoContentTypeWebP:
		return webp.Decode
	default:
		return nil
	}
}

func decodeCalendarPhotoConfig(data []byte, contentType string) (image.Config, error) {
	decoder := calendarPhotoConfigDecoder(contentType)
	if decoder == nil {
		return image.Config{}, errors.New("unsupported image format")
	}

	out, err := decoder(bytes.NewReader(data))
	if err != nil {
		return out, fmt.Errorf("decode config: %w", err)
	}

	return out, nil
}

func calendarPhotoConfigDecoder(contentType string) func(io.Reader) (image.Config, error) {
	switch contentType {
	case calendarPhotoContentTypePNG:
		return png.DecodeConfig
	case calendarPhotoContentTypeJPEG:
		return jpeg.DecodeConfig
	case calendarPhotoContentTypeWebP:
		return webp.DecodeConfig
	default:
		return nil
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
