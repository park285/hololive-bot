package api

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func TestXSpaceSessionAPIEncryptsCandidateAndRejectsConflict(t *testing.T) {
	store, err := xspaces.NewStore(dbtest.NewPool(t), bytes.Repeat([]byte{3}, 32))
	require.NoError(t, err)

	handler := &Handler{xSpaceSessions: store}
	authToken, csrfToken := strings.Repeat("a", 40), strings.Repeat("b", 64)
	body, err := jsonv2.Marshal(map[string]string{"authToken": authToken, "csrfToken": csrfToken, "expectedRevision": "0"})
	require.NoError(t, err)

	for _, accepted := range []bool{true, false} {
		ctx, recorder := newAPITestContext(http.MethodPost, "/api/holo/x-spaces/session", body)
		handler.SubmitXSpaceSession(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.NotContains(t, recorder.Body.String(), authToken)
		require.NotContains(t, recorder.Body.String(), csrfToken)

		var response xSpaceSessionResponse

		require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, accepted, response.Accepted)
		require.Equal(t, "1", response.Connection.Revision)
		require.Equal(t, "pending", response.Connection.CandidateState)
	}
}

func TestXSpaceSessionAPIRejectsOversizedOrInvalidCookies(t *testing.T) {
	handler := &Handler{}

	for _, body := range []string{
		`{"authToken":"short","csrfToken":"short","expectedRevision":"0"}`,
		`{"authToken":"` + strings.Repeat("a", 5000) + `","csrfToken":"short","expectedRevision":"0"}`,
	} {
		ctx, recorder := newAPITestContext(http.MethodPost, "/api/holo/x-spaces/session", []byte(body))
		handler.SubmitXSpaceSession(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.NotContains(t, recorder.Body.String(), "authToken")
	}

	ctx, recorder := newAPITestContext(http.MethodGet, "/api/holo/x-spaces/session", nil)
	handler.GetXSpaceSession(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response xSpaceSessionResponse

	require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Connection.Available)
	require.Equal(t, "disabled", response.Connection.State)
}
