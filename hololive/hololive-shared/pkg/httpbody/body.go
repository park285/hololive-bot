// Package httpbody provides bounded HTTP response body reads and a bounded
// drain-on-close policy so small responses remain eligible for keep-alive reuse.
package httpbody

import (
	"errors"
	"fmt"
	"io"
)

// DefaultDrainLimit bounds best-effort draining before a response body is closed.
const DefaultDrainLimit int64 = 64 << 10

var (
	// ErrNilBody reports that a response did not provide a body.
	ErrNilBody = errors.New("http response body is nil")
	// ErrTooLarge reports that a response body exceeded its configured limit.
	ErrTooLarge = errors.New("http response body exceeds limit")
)

// ReadAllAndClose reads at most maxBytes, detects an oversized body with one
// extra byte, then drains a bounded remainder and closes the body. A negative
// maxBytes is invalid; zero permits only an empty body.
func ReadAllAndClose(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, ErrNilBody
	}
	if maxBytes < 0 {
		closeErr := DrainAndClose(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("invalid response body limit %d", maxBytes), closeErr)
	}

	data, readErr := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if readErr != nil {
		closeErr := DrainAndClose(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("read response body: %w", readErr), closeErr)
	}
	if int64(len(data)) > maxBytes {
		closeErr := DrainAndClose(body, DefaultDrainLimit)
		return nil, errors.Join(fmt.Errorf("%w: max_bytes=%d", ErrTooLarge, maxBytes), closeErr)
	}
	if closeErr := DrainAndClose(body, DefaultDrainLimit); closeErr != nil {
		return nil, fmt.Errorf("close response body: %w", closeErr)
	}
	return data, nil
}

// DrainAndClose discards at most maxDrainBytes before closing body. Small
// unread responses are consumed to EOF and can be reused by net/http; large or
// unbounded responses remain bounded and are closed without an unbounded read.
func DrainAndClose(body io.ReadCloser, maxDrainBytes int64) error {
	if body == nil {
		return nil
	}
	if maxDrainBytes < 0 {
		maxDrainBytes = 0
	}
	_, drainErr := io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
	closeErr := body.Close()
	return errors.Join(drainErr, closeErr)
}
