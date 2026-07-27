package scraping

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustWriteResponse(t testing.TB, w http.ResponseWriter, body string) {
	t.Helper()
	_, err := w.Write([]byte(body))
	require.NoError(t, err)
}

func mustClose(t testing.TB, closer io.Closer) {
	t.Helper()
	require.NoError(t, closer.Close())
}
