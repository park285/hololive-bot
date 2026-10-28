package testutil

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/park285/iris-client-go/v4/iris"
)

// IrisHTTPError는 전체 HTTP 응답을 실제 SDK로 읽어 만든 오류를 반환한다.
// 표시용으로 잘린 HTTPError.Body를 재시도·재발급 증거로 합성하지 않는다.
func IrisHTTPError(ctx context.Context, t *testing.T, status int, body string) error {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	}))

	defer server.Close()

	client, err := iris.NewClient(
		iris.WithBaseURL(server.URL),
		iris.WithBotToken("test-token"),
		iris.WithTransport(iris.TransportHTTP1),
		iris.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("new iris client: %v", err)
	}

	_, err = client.SendMessageAccepted(ctx, "1", "error fixture")
	if err == nil {
		t.Fatal("expected SDK HTTP error")
	}

	return err
}
