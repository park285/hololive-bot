package htmlscraper

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestOfficialScheduleAPINilResponse(t *testing.T) {
	service := newTestServiceWithHTTPClient(
		t,
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			//nolint:nilnil // (nil 응답, nil 오류) 조합 자체가 이 테스트의 검증 대상이라 sentinel 오류로 바꿀 수 없다.
			return nil, nil
		})},
		slog.Default(),
		"https://schedule.example",
	)

	_, err := service.fetchOfficialScheduleAPI(t.Context())
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}

	if got := err.Error(); !strings.Contains(got, "nil *Response") && !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
