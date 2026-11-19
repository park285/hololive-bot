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

	return string(data), nil
}

func (t *ThumbnailsJSON) Scan(value any) error {
	if value == nil {
		*t = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("scan ThumbnailsJSON: expected []byte, got %T", value)
	}

	if err := jsonv2.Unmarshal(bytes, t); err != nil {
		return fmt.Errorf("unmarshal thumbnails: %w", err)
	}

	return nil
}
