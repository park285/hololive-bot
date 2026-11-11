package scheduleposter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/stretchr/testify/require"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const sophiaUserID = "2054825907916054528"

type fakeSources struct {
	pages    map[string]string
	cursors  []string
	original source
	err      error
}

func (f *fakeSources) statuses(_ context.Context, userID, cursor string) (statusPage, error) {
	if userID != sophiaUserID {
		return statusPage{}, errors.New("unexpected user")
	}

	f.cursors = append(f.cursors, cursor)

	name, ok := f.pages[cursor]
	if !ok {
		return statusPage{}, errors.New("unexpected cursor " + cursor)
	}

	raw, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		return statusPage{}, fmt.Errorf("read status fixture: %w", err)
	}

	return decodeStatusPage(raw)
}

func (f *fakeSources) image(context.Context, string) (source, error) {
	return f.original, f.err
}

type fakeStore struct {
	inserted []posterstore.Candidate
	pending  []posterstore.PendingTranslation
	recorded []posterstore.Image
	failures []string
}

func (f *fakeStore) InsertCandidates(_ context.Context, candidates []posterstore.Candidate) (int64, error) {
	f.inserted = append(f.inserted, candidates...)

	return int64(len(candidates)), nil
}

func (f *fakeStore) ExpirePending(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeStore) Purge(context.Context, time.Time, int) (int64, error) { return 0, nil }

func (f *fakeStore) PendingTranslations(context.Context, int) ([]posterstore.PendingTranslation, error) {
	return f.pending, nil
}

func (f *fakeStore) RecordTranslation(_ context.Context, _ posterstore.PendingTranslation, translated posterstore.Image) (bool, error) {
	f.recorded = append(f.recorded, translated)

	return true, nil
}

func (f *fakeStore) RecordTranslationFailure(_ context.Context, _ posterstore.PendingTranslation, reason string) (bool, error) {
	f.failures = append(f.failures, reason)

	return true, nil
}

type fakeTranslator struct {
	output source
	err    error
	calls  int
}

func (f *fakeTranslator) translate(context.Context, source) (source, error) {
	f.calls++

	return f.output, f.err
}

func testService(store *fakeStore, sources sourceClient, translator imageTranslator) *Service {
	config := Config{PollSeconds: 3600, Targets: []Target{{XUserID: sophiaUserID, ChannelID: "UCROQtXcp2loQEmvpe5rhJzQ"}}}
	service := newService(config, store, store, sources, translator, slog.New(slog.DiscardHandler))

	service.now = func() time.Time { return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC) }
	service.retry = retry.RetryOptions{MaxAttempts: 3, Sleep: func(context.Context, time.Duration) bool { return true }}

	return service
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buffer bytes.Buffer

	require.NoError(t, png.Encode(&buffer, canvas))

	return buffer.Bytes()
}

func TestCollectKeepsOnlyRecentOwnPosterOriginals(t *testing.T) {
	t.Parallel()

	sources := &fakeSources{pages: map[string]string{
		"":      "statuses_page1.json",
		"page2": "statuses_page2.json",
		"page3": "statuses_page3.json",
	}}
	store := &fakeStore{}

	require.NoError(t, testService(store, sources, &fakeTranslator{}).collect(t.Context()))

	require.Equal(t, []string{"", "page2", "page3"}, sources.cursors, "8일보다 오래된 본인 원글만 남은 페이지에서 멈춘다")
	require.Equal(t, []posterstore.Candidate{{
		ID:          2119784604752343207,
		ChannelID:   "UCROQtXcp2loQEmvpe5rhJzQ",
		XUserID:     sophiaUserID,
		PostURL:     "https://x.com/i/status/2119784604752343207",
		ImageURL:    "https://pbs.twimg.com/media/HWr8oRIa0AAlqaZ.jpg?name=orig",
		TextExcerpt: "🌟週間スケジュール🌟\n　11/9(月) ～ 11/15(日)",
		PostedAt:    time.Unix(1794231008, 0),
	}}, store.inserted)
}

func TestTranslatePendingStoresJPEG(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ImageURL: "https://pbs.twimg.com/media/a.jpg", Revision: 2}}}
	translator := &fakeTranslator{output: source{MIME: "image/png", Data: pngBytes(t, 64, 36)}}
	sources := &fakeSources{original: source{MIME: "image/jpeg", Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Len(t, store.recorded, 1)
	require.Equal(t, "image/jpeg", store.recorded[0].MIME)

	decoded, err := jpeg.DecodeConfig(bytes.NewReader(store.recorded[0].Data))
	require.NoError(t, err)
	require.Equal(t, 64, decoded.Width)
	require.Empty(t, store.failures)
}

func TestTranslatePendingRecordsFailureAfterRetries(t *testing.T) {
	t.Parallel()

	store := &fakeStore{pending: []posterstore.PendingTranslation{{ID: 1, ImageURL: "https://pbs.twimg.com/media/a.jpg", Revision: 2}}}
	translator := &fakeTranslator{err: errors.New("model unavailable")}
	sources := &fakeSources{original: source{MIME: "image/jpeg", Data: []byte("original")}}

	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))

	require.Equal(t, 3, translator.calls)
	require.Equal(t, []string{"translation_failed"}, store.failures)
	require.Empty(t, store.recorded)

	sources.err = errors.New("gone")
	require.NoError(t, testService(store, sources, translator).translatePending(t.Context()))
	require.Equal(t, []string{"translation_failed", "source_unavailable"}, store.failures)
}

func TestToJPEGRejectsOversizedImages(t *testing.T) {
	t.Parallel()

	_, err := toJPEG(source{MIME: "image/png", Data: pngBytes(t, maxImageDimension+1, 8)})
	require.Error(t, err)

	_, err = toJPEG(source{MIME: "image/png", Data: []byte("not an image")})
	require.Error(t, err)

	_, err = toJPEG(source{MIME: "image/gif", Data: pngBytes(t, 8, 8)})
	require.Error(t, err)
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig("")
	require.ErrorIs(t, err, ErrDisabled)

	_, err = LoadConfig("relative.json")
	require.Error(t, err)

	write := func(body string) string {
		path := filepath.Join(t.TempDir(), "schedule-poster.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}

	config, err := LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project"}}`))
	require.NoError(t, err)
	require.Equal(t, time.Hour, config.PollInterval())
	require.Equal(t, TranslationConfig{Project: "example-project", Location: "global", Model: "gemini-nano-banana-2.1"}, config.Translation)

	_, err = LoadConfig(write(`{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}],"translation":{"project":"example-project"},"api_key":"x"}`))
	require.Error(t, err, "모르는 키는 거부한다")

	_, err = LoadConfig(write(`{"targets":[],"translation":{"project":"example-project"}}`))
	require.Error(t, err)
}
