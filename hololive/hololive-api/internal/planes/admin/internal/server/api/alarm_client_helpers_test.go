package api

import (
	"testing"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

func newTestAlarmClient(t *testing.T, baseURL string) *alarmhttp.Client {
	t.Helper()

	client, err := alarmhttp.NewClientWithAPIKeyStrict(baseURL, "", newDiscardLogger(), sharedh3.ClientOptions{})
	if err != nil {
		t.Fatalf("new alarm client: %v", err)
	}

	return client
}
