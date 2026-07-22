package scraper

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type trackingReadCloser struct {
	reader   *strings.Reader
	reads    int
	closed   bool
	readErr  error
	closeErr error
}

func newTrackingReadCloser(body string) *trackingReadCloser {
	return &trackingReadCloser{reader: strings.NewReader(body)}
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}

func TestFeedFetcherReadResponseBodyAcceptsExactLimit(t *testing.T) {
	body := newTrackingReadCloser("1234")
	fetcher := &FeedFetcher{maxBodyLen: 4}

	got, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 4,
		Body:          body,
	})

	require.NoError(t, err)
	require.Equal(t, []byte("1234"), got)
	require.Positive(t, body.reads)
	require.True(t, body.closed)
}

func TestFeedFetcherReadResponseBodyRejectsUnknownLengthOverflow(t *testing.T) {
	body := newTrackingReadCloser("12345")
	closeErr := errors.New("close failed")
	body.closeErr = closeErr
	fetcher := &FeedFetcher{maxBodyLen: 4}

	_, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: -1,
		Body:          body,
	})

	require.ErrorContains(t, err, "body exceeds 4 bytes")
	require.ErrorIs(t, err, closeErr)
	require.Positive(t, body.reads)
	require.True(t, body.closed)
}

func TestFeedFetcherReadResponseBodyRejectsAdvertisedOverflowBeforeRead(t *testing.T) {
	body := newTrackingReadCloser("12345")
	fetcher := &FeedFetcher{maxBodyLen: 4}

	_, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 5,
		Body:          body,
	})

	require.ErrorContains(t, err, "body exceeds 4 bytes")
	require.Zero(t, body.reads)
	require.True(t, body.closed)
}

func TestFeedFetcherReadResponseBodyRejectsErrorStatusBeforeRead(t *testing.T) {
	body := newTrackingReadCloser("error body")
	fetcher := &FeedFetcher{maxBodyLen: 4}

	_, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusInternalServerError,
		ContentLength: 10,
		Body:          body,
	})

	require.ErrorContains(t, err, "unexpected status 500")
	require.Zero(t, body.reads)
	require.True(t, body.closed)
}

func TestFeedFetcherReadResponseBodyPreservesReadAndCloseErrors(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	body := newTrackingReadCloser("")
	body.readErr = readErr
	body.closeErr = closeErr
	fetcher := &FeedFetcher{maxBodyLen: 4}

	_, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: -1,
		Body:          body,
	})

	require.ErrorIs(t, err, readErr)
	require.ErrorIs(t, err, closeErr)
	require.Positive(t, body.reads)
	require.True(t, body.closed)
}

func TestFeedFetcherReadResponseBodyReturnsCloseErrorAfterRead(t *testing.T) {
	closeErr := errors.New("close failed")
	body := newTrackingReadCloser("1234")
	body.closeErr = closeErr
	fetcher := &FeedFetcher{maxBodyLen: 4}

	got, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 4,
		Body:          body,
	})

	require.ErrorIs(t, err, closeErr)
	require.Nil(t, got)
	require.Positive(t, body.reads)
	require.True(t, body.closed)
}
