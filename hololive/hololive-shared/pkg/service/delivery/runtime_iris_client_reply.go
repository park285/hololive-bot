package delivery

import (
	"context"
	"fmt"

	"github.com/park285/iris-client-go/iris"
)

func (c *RuntimeIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runtime iris client: client is nil")
	}
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendMessageAccepted(ctx, room, message, opts...)
}
