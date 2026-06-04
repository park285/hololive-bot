package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var photoClient = &http.Client{Timeout: 5 * time.Second}

const (
	photoFetchBudget           = 15 * time.Second
	calendarPhotoThumbnailSize = 1024
	calendarPhotoMaxBytes      = 2 << 20
)

func fetchMemberPhotos(entries []domain.CalendarEntry) map[string]image.Image {
	deadline := time.Now().Add(photoFetchBudget)
	photos := make(map[string]image.Image)
	for _, e := range entries {
		if time.Now().After(deadline) {
			break
		}
		fetchMemberPhoto(e, photos)
	}
	return photos
}

func fetchMemberPhoto(e domain.CalendarEntry, photos map[string]image.Image) {
	if e.Member == nil || e.Member.Photo == "" {
		return
	}
	if _, ok := photos[e.Member.Photo]; ok {
		return
	}
	url := thumbnailURL(e.Member.Photo, calendarPhotoThumbnailSize)
	if img, err := fetchImage(url); err == nil {
		photos[e.Member.Photo] = img
	}
}

func thumbnailURL(original string, size int) string {
	if before, _, ok := strings.Cut(original, "=s"); ok {
		return fmt.Sprintf("%s=s%d-c-k-c0x00ffffff-no-rj", before, size)
	}
	return original
}

func fetchImage(url string) (image.Image, error) {
	resp, err := photoClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, calendarPhotoMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > calendarPhotoMaxBytes {
		return nil, errors.New("image exceeds calendar photo byte limit")
	}
	if img, err := png.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	if img, err := jpeg.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	return nil, fmt.Errorf("unsupported image format")
}
