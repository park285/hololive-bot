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

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/image/webp"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

var photoClient = newCalendarPhotoHTTPClient()

const (
	photoFetchBudget           = 15 * time.Second
	calendarPhotoMaxFetches    = 24
	calendarPhotoFetchWorkers  = 4
	calendarPhotoThumbnailSize = 1024
	calendarPhotoMaxBytes      = 2 << 20
	calendarPhotoMaxDimension  = 4096
	calendarPhotoMaxPixels     = 8 << 20
)

type calendarPhotoFetchResult struct {
	photos      map[string]image.Image
	cachePolicy calendarCachePolicy
}

type calendarPhotoFetchSlot struct {
	image         image.Image
	ok            bool
	budgetExpired bool
}

func fetchMemberPhotos(parent context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
	photoURLs, truncated := uniqueCalendarPhotoURLs(entries, calendarPhotoMaxFetches)

	return fetchPhotos(parent, photoURLs, truncated)
}

func fetchPhotos(parent context.Context, photoURLs []string, truncated bool) (calendarPhotoFetchResult, error) {
	budgetCtx, cancel := context.WithTimeout(parent, photoFetchBudget)

	defer cancel()

	out, err := fetchPhotosWithinBudget(parent, budgetCtx, photoURLs, truncated)
	if err != nil {
		return out, fmt.Errorf("fetch member photos within budget: %w", err)
	}

	return out, nil
}

func fetchPhotosWithinBudget(parent, budgetCtx context.Context, photoURLs []string, truncated bool) (calendarPhotoFetchResult, error) {
	result := calendarPhotoFetchResult{
		photos: make(map[string]image.Image),
		cachePolicy: calendarCachePolicy{
			memoryCacheable: true,
			diskCacheable:   true,
		},
	}

	if err := parent.Err(); err != nil {
		return result, fmt.Errorf("before member photo download: %w", err)
	}

	if truncated {
		result.cachePolicy.markUncacheable()
	}

	slots := make([]calendarPhotoFetchSlot, len(photoURLs))

	var group errgroup.Group

	group.SetLimit(calendarPhotoFetchWorkers)

	for i, photoURL := range photoURLs {
		group.Go(func() error {
			return panicguard.RunE(nil, panicscope.BackgroundTask, "calendar-photo-fetch", func() error {
				slots[i] = fetchCalendarPhotoSlot(budgetCtx, photoURL)

				return nil
			})
		})
	}

	if err := group.Wait(); err != nil {
		result.cachePolicy.markDiskUncacheable()
	}

	if err := parent.Err(); err != nil {
		return result, fmt.Errorf("after member photo downloads: %w", err)
	}

	for i, photoURL := range photoURLs {
		slot := slots[i]
		if slot.ok {
			result.photos[photoURL] = slot.image

			continue
		}

		result.cachePolicy.markDiskUncacheable()

		if slot.budgetExpired {
			result.cachePolicy.markUncacheable()
		}
	}

	return result, nil
}

func uniqueCalendarPhotoURLs(entries []domain.CalendarEntry, limit int) (photoURLs []string, truncated bool) {
	seen := make(map[string]struct{}, min(len(entries), limit))

	for _, e := range entries {
		photoURL, ok := calendarEntryPhotoURL(e)
		if !ok {
			continue
		}

		if _, ok := seen[photoURL]; ok {
			continue
		}

		if len(photoURLs) >= limit {
			return photoURLs, true
		}

		seen[photoURL] = struct{}{}
		photoURLs = append(photoURLs, photoURL)
	}

	return photoURLs, false
}

func fetchCalendarPhotoSlot(ctx context.Context, photoURL string) calendarPhotoFetchSlot {
	img, ok := fetchMemberPhoto(ctx, photoURL)
	if !ok {
		return calendarPhotoFetchSlot{budgetExpired: ctx.Err() != nil}
	}

	return calendarPhotoFetchSlot{image: img, ok: true}
}

func calendarEntryPhotoURL(e domain.CalendarEntry) (string, bool) {
	if e.Member == nil || e.Member.Photo == "" {
		return "", false
	}

	return e.Member.Photo, true
}

func (p *calendarCachePolicy) markDiskUncacheable() {
	p.diskCacheable = false
}

func (p *calendarCachePolicy) markUncacheable() {
	p.memoryCacheable = false
	p.diskCacheable = false
}

func fetchMemberPhoto(ctx context.Context, photoURL string) (image.Image, bool) {
	url := thumbnailURL(photoURL, calendarPhotoThumbnailSize)

	img, err := fetchImageWithContext(ctx, url)
	if err != nil {
		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			slog.Debug("calendar photo fetch skipped",
				slog.String("photo_host", calendarPhotoURLHost(url)),
				slog.String("err", err.Error()),
			)
		}

		return nil, false
	}

	return img, true
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
	data, err := httputil.ReadAllLimited(body, calendarPhotoMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("read calendar photo: %w", err)
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
