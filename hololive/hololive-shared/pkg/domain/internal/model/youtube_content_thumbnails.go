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

package model

import (
	"database/sql/driver"
	"fmt"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type ThumbnailsJSON []ThumbnailEntry

type ThumbnailEntry struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func (t ThumbnailsJSON) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("marshal thumbnails: %w", err)
	}
	// pgx stdlib 드라이버는 []byte를 bytea로 해석하므로, jsonb 컬럼에는 string으로 반환해야 한다.
	return string(data), nil
}

func (t *ThumbnailsJSON) Scan(value any) error {
	if value == nil {
		*t = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan ThumbnailsJSON: expected []byte, got %T", value)
	}
	if err := json.Unmarshal(bytes, t); err != nil {
		return fmt.Errorf("unmarshal thumbnails: %w", err)
	}
	return nil
}

var YouTubeModels = []any{
	&YouTubeChannelStatsSnapshot{},
	&YouTubeChannelProfile{},
	&YouTubeVideo{},
	&YouTubeCommunityPost{},
	&YouTubeContentWatermark{},
	&YouTubeNotificationOutbox{},
	&YouTubeContentAlarmTracking{},
	&YouTubeCommunityShortsSourcePost{},
	&YouTubeCommunityShortsAlarmState{},
	&YouTubeCommunityShortsObservationWindow{},
	&YouTubeNotificationDeliveryTelemetry{},
	&YouTubeNotificationDelivery{},
	&YouTubeLiveSession{},
	&YouTubeLiveViewerSample{},
	&YouTubeStreamStats{},
}
