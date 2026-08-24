// Package httpbody는 HTTP response body의 read와 close 전 drain을 제한한다.
package httpbody

import (
	"errors"
	"fmt"
	"io"
)

// DefaultDrainLimit은 response body를 닫기 전 drain 상한이다.
const DefaultDrainLimit int64 = 64 << 10

var (
	// ErrNilBody는 response body가 없음을 나타낸다.
	ErrNilBody = errors.New("http response body is nil")
	// ErrTooLarge는 response body가 설정한 상한을 초과했음을 나타낸다.
	ErrTooLarge = errors.New("http response body exceeds limit")
)

// ReadAllAndClose는 maxBytes와 초과 탐지용 1바이트를 읽고 남은 body를 제한해 drain한다.
// 음수 상한은 잘못된 값이며 0은 빈 body만 허용한다.
func ReadAllAndClose(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, ErrNilBody
	}

	data, readErr := ReadAllAndDrain(body, maxBytes)
	closeErr := body.Close()

	if readErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}

	if closeErr != nil {
		return nil, fmt.Errorf("close response body: %w", closeErr)
	}

	return data, nil
}

// ReadAllAndDrain은 response body를 닫지 않고 bounded read와 drain까지만 수행한다.
func ReadAllAndDrain(body io.Reader, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, ErrNilBody
	}

	if maxBytes < 0 {
		drainErr := drain(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("invalid response body limit %d", maxBytes), drainErr)
	}

	data, readErr := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if readErr != nil {
		drainErr := drain(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("read response body: %w", readErr), drainErr)
	}

	if int64(len(data)) > maxBytes {
		drainErr := drain(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("%w: max_bytes=%d", ErrTooLarge, maxBytes), drainErr)
	}

	return data, nil
}

// DrainAndClose는 설정된 상한과 EOF 확인용 1바이트까지만 버린 뒤 body를 닫는다.
// 작은 응답은 EOF까지 소비해 net/http 연결 재사용을 허용하고 큰 응답은 제한한다.
func DrainAndClose(body io.ReadCloser, maxDrainBytes int64) error {
	if body == nil {
		return nil
	}

	drainErr := drain(body, maxDrainBytes)
	closeErr := body.Close()

	return errors.Join(drainErr, closeErr)
}

// Drain은 response body를 닫지 않고 설정된 상한까지만 버린다.
func Drain(body io.Reader, maxDrainBytes int64) error {
	if err := drain(body, maxDrainBytes); err != nil {
		return fmt.Errorf("drain: %w", err)
	}

	return nil
}

func drain(body io.Reader, maxDrainBytes int64) error {
	if body == nil || maxDrainBytes <= 0 {
		return nil
	}

	drainLimit := maxDrainBytes
	if maxDrainBytes != int64(^uint64(0)>>1) {
		drainLimit++
	}

	if _, err := io.Copy(io.Discard, io.LimitReader(body, drainLimit)); err != nil {
		return fmt.Errorf("drain body: %w", err)
	}

	return nil
}
