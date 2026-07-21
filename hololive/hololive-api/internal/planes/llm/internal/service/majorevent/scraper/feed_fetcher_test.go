package scraper

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type trackingReadCloser struct {
	reader *strings.Reader
	reads  int
	closed bool
}

func newTrackingReadCloser(body string) *trackingReadCloser {
	return &trackingReadCloser{reader: strings.NewReader(body)}
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
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
	fetcher := &FeedFetcher{maxBodyLen: 4}

	_, err := fetcher.readResponseBody(&http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: -1,
		Body:          body,
	})

	require.ErrorContains(t, err, "body exceeds 4 bytes")
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
