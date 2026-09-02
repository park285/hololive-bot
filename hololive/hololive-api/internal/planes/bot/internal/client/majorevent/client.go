package majorevent

import (
	"time"

	"github.com/kapu/hololive-api/internal/service/subscriptionclient"
	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
)

type Client struct {
	subscriptionclient.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		HTTPClient:        internalhttp.NewJSONClient(baseURL, apiKey, 30*time.Second, nil),
		SubscriptionsPath: majoreventcontracts.SubscriptionsPath,
	}
}
