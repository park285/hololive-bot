package alarm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountHTTPRoundTripDoesNotLoadAlarmList(t *testing.T) {
	for _, fail := range []bool{false, true} {
		mock := &mockAlarmCRUD{countAlarmEntriesFn: func(context.Context) (int, error) {
			if fail {
				return 0, errors.New("database unavailable")
			}

			return 3, nil
		}}
		server := httptest.NewServer(newTestHandler(t, mock))
		t.Cleanup(server.Close)

		client := NewClient(server.URL, nil)
		count, err := client.CountAlarmEntries(t.Context())

		if fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, 3, count)
		}
	}
}

func TestCountClientRejectsMissingNullAndNegativeValues(t *testing.T) {
	for _, body := range []string{`{"success":true,"data":{}}`, `{"success":true,"data":{"count":null}}`, `{"success":true,"data":{"count":-1}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if _, err := w.Write([]byte(body)); err != nil {
				t.Error(err)
			}
		}))
		t.Cleanup(server.Close)

		_, err := NewClient(server.URL, nil).CountAlarmEntries(t.Context())
		require.Error(t, err)
	}
}
