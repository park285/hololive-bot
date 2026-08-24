package scraping

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustWriteResponse(tb testing.TB, w http.ResponseWriter, body string) {
	tb.Helper()

	_, err := w.Write([]byte(body))
	require.NoError(tb, err)
}

func mustClose(tb testing.TB, closer io.Closer) {
	tb.Helper()
	require.NoError(tb, closer.Close())
}
