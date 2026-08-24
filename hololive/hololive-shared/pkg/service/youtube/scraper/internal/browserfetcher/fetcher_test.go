package browserfetcher

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type nilResponseTransport struct{}

//nolint:nilnil // FetchPage의 nil 응답 방어 코드를 타게 하려면 이 스텁이 (nil, nil)을 그대로 돌려줘야 한다.
func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestFetchPageNilResponse(t *testing.T) {
	fetcher := New("https://browser.example/snapshot", time.Second)

	fetcher.client.Transport = nilResponseTransport{}

	_, err := fetcher.FetchPage(t.Context(), Request{URL: "https://youtube.example/watch?v=test"})
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}

	if got := err.Error(); !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
