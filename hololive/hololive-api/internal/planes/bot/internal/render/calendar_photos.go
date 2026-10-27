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

// calendarPhotoFetchSlot은 사진 하나의 다운로드 결과다. 각 goroutine은 자기 index의 slot에만 쓰고, 쓰지 못한 slot의
// zero value는 실패로 집계된다.
type calendarPhotoFetchSlot struct {
	image         image.Image
	ok            bool
	budgetExpired bool
}

func fetchMemberPhotos(parent context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
	budgetCtx, cancel := context.WithTimeout(parent, photoFetchBudget)

	defer cancel()

	out, err := fetchMemberPhotosWithinBudget(parent, budgetCtx, entries)
	if err != nil {
		return out, fmt.Errorf("fetch member photos within budget: %w", err)
	}

	return out, nil
}

// fetchMemberPhotosWithinBudget은 entries의 고유 사진을 최대 calendarPhotoMaxFetches장까지 calendarPhotoFetchWorkers개씩
// 동시에 받는다. 캐시 정책은 다음과 같다. 상한을 넘는 사진이 있거나 예산이 끝난 뒤 실패한 사진이 있으면 메모리·디스크
// 캐시를 모두 막고, 그 밖의 개별 실패는 디스크 캐시만 막는다. 부모 ctx 취소는 오류로 돌려준다.
func fetchMemberPhotosWithinBudget(parent, budgetCtx context.Context, entries []domain.CalendarEntry) (calendarPhotoFetchResult, error) {
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

	photoURLs, truncated := uniqueCalendarPhotoURLs(entries, calendarPhotoMaxFetches)
	if truncated {
		result.cachePolicy.markUncacheable()
	}

	slots := make([]calendarPhotoFetchSlot, len(photoURLs))

	// 개별 다운로드 실패는 slot에만 남는다. group에는 context를 묶지 않으므로 한 장의 실패나 panic이 나머지 다운로드를
	// 멈추지 않는다.
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

	// panic은 panicguard가 stack과 함께 기록했고, 그 사진의 slot은 실패로 남아 아래에서 디스크 캐시를 막는다.
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

// uniqueCalendarPhotoURLs는 entries 순서대로 고유 사진 URL을 limit개까지 모은다. 상한 limit을 넘는 고유 URL이 있으면
// truncated가 참이다. 이미 모은 URL의 중복과 사진 없는 항목은 상한을 넘긴 것으로 보지 않는다.
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

// fetchMemberPhoto는 사진 URL의 고해상도 썸네일을 받아 디코드한다. 실패는 calendar에서 사진 없이 그리는 정상 경로라
// Debug로만 남긴다.
func fetchMemberPhoto(ctx context.Context, photoURL string) (image.Image, bool) {
	url := thumbnailURL(photoURL, calendarPhotoThumbnailSize)

	img, err := fetchImageWithContext(ctx, url)
	if err != nil {
		slog.Debug("calendar photo fetch skipped",
			slog.String("photo_host", calendarPhotoURLHost(url)),
			slog.String("err", err.Error()),
		)

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
