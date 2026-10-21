package api

import (
	"testing"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

// newTestAlarmClient는 운영 생성자로 httptest worker를 가리키는 알람 client를 만든다.
func newTestAlarmClient(t *testing.T, baseURL string) *alarmhttp.Client {
	t.Helper()

	client, err := alarmhttp.NewClientWithAPIKeyStrict(baseURL, "", newDiscardLogger(), sharedh3.ClientOptions{})
	if err != nil {
		t.Fatalf("new alarm client: %v", err)
	}

	return client
}
