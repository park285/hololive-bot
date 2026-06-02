package workerapp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/iris"
)

func TestIsAlarmDispatchRetryablePostSendFailure_TypedHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typed 502 direct",
			err:  &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"},
			want: true,
		},
		{
			name: "typed 503 direct",
			err:  &iris.HTTPError{StatusCode: 503, URL: "/karing/content-list"},
			want: true,
		},
		{
			name: "typed 502 wrapped through fmt.Errorf",
			err:  fmt.Errorf("iris send karing content list: %w", fmt.Errorf("send iris karing content list: %w", &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"})),
			want: true,
		},
		{
			name: "typed 503 wrapped through fmt.Errorf",
			err:  fmt.Errorf("iris send karing content list: %w", &iris.HTTPError{StatusCode: 503, URL: "/karing/content-list"}),
			want: true,
		},
		{
			// 502 에러지만 URL이 /karing/content-list가 아닌 다른 엔드포인트 — strings.Contains는 false를 반환하지만 errors.As는 StatusCode만 봐야 함
			name: "typed 502 on different URL should also be retryable",
			err:  &iris.HTTPError{StatusCode: 502, URL: "/some-other-endpoint"},
			want: true,
		},
		{
			// 503 에러, URL 없음 — strings.Contains는 URL 패턴에 의존하므로 false 반환
			name: "typed 503 with empty URL should be retryable",
			err:  &iris.HTTPError{StatusCode: 503},
			want: true,
		},
		{
			name: "typed 500 not retryable",
			err:  &iris.HTTPError{StatusCode: 500, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "typed 400 not retryable",
			err:  &iris.HTTPError{StatusCode: 400, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("something else failed"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isAlarmDispatchRetryablePostSendFailure(tc.err)
			if got != tc.want {
				t.Errorf("isAlarmDispatchRetryablePostSendFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
