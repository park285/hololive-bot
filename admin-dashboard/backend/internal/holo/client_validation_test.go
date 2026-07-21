package holo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeHoloBaseURLRejectsNonOriginURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "relative path", url: "/internal"},
		{name: "opaque host port", url: "hololive-api:30006"},
		{name: "missing host", url: "https://"},
		{name: "unsupported scheme", url: "ftp://hololive-api"},
		{name: "embedded credentials", url: "https://admin:secret@hololive-api"},
		{name: "query", url: "https://hololive-api?mode=debug"},
		{name: "empty query", url: "https://hololive-api?"},
		{name: "fragment", url: "https://hololive-api#internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeHoloBaseURL(tt.url)
			require.Error(t, err)
		})
	}
}

func TestNormalizeHoloBaseURLCanonicalizesSupportedURL(t *testing.T) {
	t.Parallel()

	got, err := normalizeHoloBaseURL("  HTTPS://hololive-api:30006/internal/  ")
	require.NoError(t, err)
	require.Equal(t, "https://hololive-api:30006/internal", got)
}
