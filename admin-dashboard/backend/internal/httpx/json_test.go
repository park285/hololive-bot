package httpx

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestJSONResponsePreservesWireBoundary(t *testing.T) {
	t.Parallel()

	data := struct {
		ID   string   `json:"id"`
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}{ID: "9007199254740993", Name: "한글 <>&\x01"}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	Respond(c, http.StatusCreated, data)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"id":"9007199254740993","name":"한글 \u003c\u003e\u0026\u0001","tags":[]}`, rec.Body.String())
	require.Contains(t, rec.Body.String(), `\u003c\u003e\u0026\u0001`)
	require.Equal(t, strconv.Itoa(rec.Body.Len()), rec.Header().Get("Content-Length"))
}

func TestJSONResponseEncodingFailureWritesNothing(t *testing.T) {
	t.Parallel()

	for _, value := range []any{"invalid\xffUTF8", make(chan int)} {
		rec := httptest.NewRecorder()
		require.Error(t, (jsonResponse{data: value}).Render(rec))
		require.Empty(t, rec.Body.String())
		require.Empty(t, rec.Header().Get("Content-Type"))
	}
}

func TestJSONResponsePreservesNoContentAndExistingHeader(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/problem+json")

	c, _ := gin.CreateTestContext(rec)
	Respond(c, http.StatusNoContent, make(chan int))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Body.String())
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

type failedJSONWriter struct {
	*httptest.ResponseRecorder

	err error
}

func (w failedJSONWriter) Write([]byte) (int, error) { return 0, w.err }

func TestJSONResponseReturnsWriteFailure(t *testing.T) {
	t.Parallel()

	cause := errors.New("fixture response disconnected")
	require.ErrorIs(t, (jsonResponse{data: "valid"}).Render(failedJSONWriter{httptest.NewRecorder(), cause}), cause)
}

func TestJSONResponseConcurrentBuffersRemainIsolated(t *testing.T) {
	t.Parallel()

	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()

			text := strings.Repeat(fmt.Sprintf("한글-%d-", i), 4096)

			for range 4 {
				rec := httptest.NewRecorder()
				require.NoError(t, (jsonResponse{data: text}).Render(rec))
				require.Equal(t, `"`+text+`"`, rec.Body.String())
			}
		})
	}
}

func TestJSONResponseDoesNotReusePartialOrPreviousBody(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 2<<20), "valid"} {
		rec := httptest.NewRecorder()
		require.Error(t, (jsonResponse{data: []string{text, "invalid\xffUTF8"}}).Render(rec))
		require.Empty(t, rec.Body.String())

		rec = httptest.NewRecorder()
		require.NoError(t, (jsonResponse{data: "next"}).Render(rec))
		require.Equal(t, `"next"`, rec.Body.String())
	}
}

type partialJSONResponse struct{ err error }

func (r partialJSONResponse) WriteJSONTo(w io.Writer) error {
	if _, err := io.WriteString(w, `{"status":`); err != nil {
		return err
	}

	return r.err
}

func TestJSONResponseWithholdsFailedCustomEncoding(t *testing.T) {
	cause := errors.New("fixture encoding failed")
	rec := httptest.NewRecorder()
	require.ErrorIs(t, (jsonResponse{data: partialJSONResponse{cause}}).Render(rec), cause)
	require.Empty(t, rec.Body.Bytes())
	require.Empty(t, rec.Header().Get("Content-Length"))
}
