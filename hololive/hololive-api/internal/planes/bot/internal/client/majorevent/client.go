package majorevent

import (
	"fmt"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-api/internal/service/subscriptionclient"
	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type Client struct {
	subscriptionclient.Client
}

// New는 llm-scheduler major event 구독 client를 만든다. URL이 https이고 H3 transport를 구성하지 못하면 오류다.
func New(baseURL, apiKey string, options sharedh3.ClientOptions) (*Client, error) {
	httpClient, err := internalhttp.NewJSONClient(baseURL, apiKey, 30*time.Second, options)
	if err != nil {
		return nil, fmt.Errorf("configure major event client transport: %w", err)
	}

	return &Client{
		HTTPClient:        httpClient,
		SubscriptionsPath: majoreventcontracts.SubscriptionsPath,
	}, nil
}
