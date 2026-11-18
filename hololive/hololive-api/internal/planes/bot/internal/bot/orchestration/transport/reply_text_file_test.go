package transport

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func TestTextFileReplayPreservesThreadIdentityAndUTF8(t *testing.T) {
	const schedule = "2026-11-09\n18:00 방송\n비고: 마지막 문장\n"

	writer := &recordingReplyOutbox{}
	tr := NewCommandTransport(writer, nil)
	ctx := WithThreadID(inboundCtx(t.Context()), "456")
	require.NoError(t, tr.SendTextFile(ctx, "123", "schedule.txt", schedule))
	require.Len(t, writer.entries, 1)

	entry := writer.entries[0]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/auth/epoch" {
			_, err := fmt.Fprint(w, `{"epoch":"0123456789abcdef0123456789abcdef"}`)
			assert.NoError(t, err)

			return
		}

		if err := checkScheduleUpload(t, w, r, entry.ClientRequestID, schedule); err != nil {
			t.Errorf("read upload: %v", err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		w.WriteHeader(http.StatusAccepted)

		_, err := fmt.Fprint(w, `{"success":true,"delivery":"queued","requestId":"file-1","room":"123","type":"file"}`)
		assert.NoError(t, err)
	}))

	defer server.Close()

	client, err := delivery.NewRuntimeIrisClient(t.Context(), settings.IrisConfig{BaseURL: server.URL, BotToken: "synthetic-token"}, nil, iris.WithHTTPClient(server.Client()), iris.WithReplyRetry(0))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	for range 2 {
		id, err := DispatchStoredReply(t.Context(), client, entry.Room, []byte(entry.Payload), entry.ClientRequestID)
		require.NoError(t, err)
		require.Equal(t, "file-1", id)
	}
}

func checkScheduleUpload(t *testing.T, w http.ResponseWriter, r *http.Request, id, schedule string) error {
	t.Helper()

	r.Body = http.MaxBytesReader(w, r.Body, 65536)

	reader, err := r.MultipartReader()
	if err != nil {
		return fmt.Errorf("multipart: %w", err)
	}

	metadata, err := reader.NextPart()
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	var meta struct {
		ID     string `json:"clientRequestId"`
		Thread string `json:"threadId"`
		Room   string `json:"room"`
	}

	if decodeErr := jsonv2.UnmarshalRead(metadata, &meta); decodeErr != nil {
		return fmt.Errorf("decode metadata: %w", decodeErr)
	}

	assert.Equal(t, id, meta.ID)
	assert.Equal(t, "456", meta.Thread)
	assert.Equal(t, "123", meta.Room)

	f, err := reader.NextPart()
	if err != nil {
		return fmt.Errorf("file: %w", err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("file bytes: %w", err)
	}

	assert.Equal(t, schedule, string(data))
	assert.Equal(t, "schedule.txt", f.FileName())
	assert.Equal(t, "text/plain", f.Header.Get("Content-Type"))

	return nil
}
