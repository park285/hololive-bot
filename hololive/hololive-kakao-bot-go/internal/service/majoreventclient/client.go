package majoreventclient

import (
	"time"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/subscriptionclient"
	"github.com/park285/shared-go/pkg/httputil"
)

type Client struct {
	subscriptionclient.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		Client: subscriptionclient.Client{
			HTTPClient:        httputil.NewJSONClient(baseURL, apiKey, 30*time.Second),
			SubscriptionsPath: majoreventcontracts.SubscriptionsPath,
		},
	}
}
