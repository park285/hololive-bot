package alarmhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountClientRejectsMissingNullAndNegativeValues(t *testing.T) {
	for _, body := range []string{`{"success":true,"data":{}}`, `{"success":true,"data":{"count":null}}`, `{"success":true,"data":{"count":-1}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if _, err := w.Write([]byte(body)); err != nil {
				t.Error(err)
			}
		}))
		t.Cleanup(server.Close)

		_, err := newTestClient(server.URL, "").CountAlarmEntries(t.Context())
		require.Error(t, err)
	}
}
