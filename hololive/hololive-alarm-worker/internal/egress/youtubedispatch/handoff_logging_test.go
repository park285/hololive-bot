package youtubedispatch

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v6/iris"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestYouTubeHandoffLogsRetainDeliveryIdentity(t *testing.T) {
	fixture := sendYouTubeHandoffFixture(t)
	client := &youtubeStatusClient{snapshot: func(string) (*iris.ReplyStatusSnapshot, error) {
		return nil, &iris.HTTPError{StatusCode: http.StatusServiceUnavailable, URL: "/reply-status/" + fixture.irisRequestID}
	}}

	var output bytes.Buffer

	reconciler, err := newHandoffReconciler(
		fixture.transition,
		client,
		handoffReconcilerConfig{Horizon: testYouTubeHandoffHorizon, PollInterval: time.Second, MaxRetries: 3},
		slog.New(slog.NewJSONHandler(&output, nil)),
	)
	require.NoError(t, err)

	assertSubject := func() {
		t.Helper()

		var record struct {
			DeliveryIDs struct {
				Count   int   `json:"count"`
				FirstID int64 `json:"first_id"`
			} `json:"delivery_ids"`
		}

		require.NoError(t, jsonv2.Unmarshal(output.Bytes(), &record))
		require.Equal(t, 1, record.DeliveryIDs.Count)
		require.Equal(t, fixture.deliveryID, record.DeliveryIDs.FirstID)
		require.NotContains(t, output.String(), fixture.irisRequestID)
	}

	fixture.makeDue(t, 0)

	claimed, err := reconciler.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	requireYouTubeHandoffAwaiting(t, fixture.load(t))
	assertSubject()

	output.Reset()

	client.snapshot = youtubeStatus("outcome_unknown")

	fixture.makeDue(t, 0)

	claimed, err = reconciler.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	requireYouTubeHandoffQuarantined(t, fixture.load(t), contractsalarm.HandoffCodeOutcomeUnknown)
	assertSubject()
}
