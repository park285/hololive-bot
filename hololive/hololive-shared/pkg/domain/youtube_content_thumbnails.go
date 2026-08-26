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

package domain

import (
	"database/sql/driver"
	jsonv2 "encoding/json/v2"
	"fmt"
)

type ThumbnailsJSON []ThumbnailEntry //nolint:recvcheck // sql.Scanner는 포인터 수신자를, driver.Valuer는 값 수신자를 요구한다. Value를 포인터로 옮기면 값으로 전달되는 구조체 필드가 Valuer로 인식되지 않아 jsonb 대신 배열로 인코딩된다.

type ThumbnailEntry struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (t ThumbnailsJSON) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil //nolint:nilnil // driver.Valuer는 SQL NULL을 (nil, nil)로 표현해야 하므로 sentinel error로 대체할 수 없다.
	}

	data, err := jsonv2.Marshal(t)
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

	if err := jsonv2.Unmarshal(bytes, t); err != nil {
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
	&YouTubeNotificationDeliveryTelemetry{},
	&YouTubeNotificationDelivery{},
	&YouTubeLiveSession{},
	&YouTubeLiveViewerSample{},
	&YouTubeStreamStats{},
}
