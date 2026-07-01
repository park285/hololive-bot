package delivery

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/iris-client-go/iris"
)

const (
	runtimeIrisReplyRetryMax = 3
	// runtimeIrisReplyAttemptTimeout은 SDK 기본 per-attempt timeout(http.Client.Timeout)이며,
	// hololive는 reply 경로에서 이를 override하지 않으므로 grace 산정의 기준값이다.
	runtimeIrisReplyAttemptTimeout = 10 * time.Second
	staleClientCloseGraceMargin    = 10 * time.Second
	// defaultStaleClientCloseGrace는 rotate-out된 client에서 최악의 경우 in-flight reply보다
	// 오래 살아 있어야 한다(per-attempt timeout × retry + margin). reply retry는 캡처된 옛
	// client에 고정되므로, grace가 더 짧으면 rotation 시 in-flight reply가 끊긴다.
	defaultStaleClientCloseGrace = runtimeIrisReplyAttemptTimeout*runtimeIrisReplyRetryMax + staleClientCloseGraceMargin
)

type RuntimeIrisClient struct {
	*iris.RebindingClient
	resolver *runtimeIrisBaseURLResolver
}

func NewRuntimeIrisClient(
	fallbackBaseURL string,
	botToken string,
	baseURLFilePath string,
	logger *slog.Logger,
	opts ...iris.ClientOption,
) *RuntimeIrisClient {
	if logger == nil {
		logger = slog.Default()
	}

	clientOpts := make([]iris.ClientOption, 0, len(opts)+2)
	clientOpts = append(clientOpts, iris.WithLogger(logger), iris.WithReplyRetry(runtimeIrisReplyRetryMax))
	clientOpts = append(clientOpts, opts...)

	resolver := &runtimeIrisBaseURLResolver{
		fallbackBaseURL: strings.TrimSpace(fallbackBaseURL),
		baseURLFilePath: strings.TrimSpace(baseURLFilePath),
		logger:          logger,
	}

	rc := iris.NewRebindingClient(iris.RebindingClientConfig{
		ResolveBaseURL:  resolver.resolve,
		BotToken:        strings.TrimSpace(botToken),
		StaleCloseGrace: defaultStaleClientCloseGrace,
		Logger:          logger,
		ClientOptions:   clientOpts,
	})

	return &RuntimeIrisClient{RebindingClient: rc, resolver: resolver}
}

func (c *RuntimeIrisClient) ValidateBaseURL() error {
	_, err := c.resolver.resolve()
	return err
}

func (c *RuntimeIrisClient) SendMessageWithClientRequestID(ctx context.Context, room, message, clientRequestID string) error {
	return c.SendMessage(ctx, room, message, iris.WithClientRequestID(clientRequestID))
}
