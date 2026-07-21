package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type decodeJSONFixture struct {
	Value string `json:"value"`
}

func TestDecodeJSONAcceptsExactBodyLimit(t *testing.T) {
	payload := `{"value":"ok"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(payload))

	var dst decodeJSONFixture
	require.NoError(t, DecodeJSON(req, &dst, int64(len(payload))))
	require.Equal(t, "ok", dst.Value)
}

func TestDecodeJSONRejectsValidPrefixBeyondBodyLimit(t *testing.T) {
	payload := `{"value":"ok"}`
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/",
		strings.NewReader(payload+strings.Repeat(" ", 16)),
	)

	var dst decodeJSONFixture
	err := DecodeJSON(req, &dst, int64(len(payload)))
	require.ErrorContains(t, err, "body exceeds")
}

func TestDecodeJSONRejectsMultipleValues(t *testing.T) {
	payload := `{"value":"first"}{"value":"second"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(payload))

	var dst decodeJSONFixture
	err := DecodeJSON(req, &dst, int64(len(payload)))
	require.ErrorContains(t, err, "multiple json values")
}

func TestDecodeJSONBytesRejectsUnknownField(t *testing.T) {
	var dst decodeJSONFixture
	err := DecodeJSONBytes([]byte(`{"value":"ok","extra":true}`), &dst)
	require.Error(t, err)
}
