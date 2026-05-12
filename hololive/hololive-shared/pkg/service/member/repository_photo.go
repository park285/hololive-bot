// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package member

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (r *Repository) UpdatePhoto(ctx context.Context, channelID string, photoURL string) error {
	now := time.Now()
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("channel_id = ?", channelID).
		Updates(map[string]any{
			"photo":            photoURL,
			"photo_updated_at": now,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update photo: %w", result.Error)
	}

	return nil
}

func (r *Repository) GetPhotoByChannelID(ctx context.Context, channelID string) (string, error) {
	var member Model
	err := r.gormDB.WithContext(ctx).
		Select("photo").
		Where("channel_id = ?", channelID).
		First(&member).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get photo: %w", err)
	}

	if member.Photo == nil {
		return "", nil
	}

	return *member.Photo, nil
}

// staleThreshold: 이 기간보다 오래된 photo는 재동기화 대상
func (r *Repository) GetMembersNeedingPhotoSync(ctx context.Context, staleThreshold time.Duration) ([]string, error) {
	staleTime := time.Now().Add(-staleThreshold)

	var channelIDs []string
	err := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Select("channel_id").
		Where("channel_id IS NOT NULL").
		Where("photo IS NULL OR photo_updated_at IS NULL OR photo_updated_at < ?", staleTime).
		Pluck("channel_id", &channelIDs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get members needing photo sync: %w", err)
	}

	return channelIDs, nil
}

func UpgradePhotoResolution(photoURL string) string {
	if photoURL == "" {
		return ""
	}

	for _, size := range []string{"=s88", "=s240", "=s800", "=s176", "=s68"} {
		if strings.Contains(photoURL, size) {
			return strings.Replace(photoURL, size, "=s1024", 1)
		}
	}

	return photoURL
}
