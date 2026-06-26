package majorevent

import (
	"time"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
	"github.com/kapu/hololive-shared/pkg/service/subscriptionclient"
)

type Client struct {
	subscriptionclient.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		Client: subscriptionclient.Client{
			HTTPClient:        internalhttp.NewJSONClient(baseURL, apiKey, 30*time.Second, nil),
			SubscriptionsPath: majoreventcontracts.SubscriptionsPath,
		},
	}
}
