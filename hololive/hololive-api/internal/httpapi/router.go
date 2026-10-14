package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func NewTriggerRuntimeRouter(
	ctx context.Context,
	logger *slog.Logger,
	triggerHandler *TriggerHandler,
	apiKey string,
	opts ...func(*sharedserver.RuntimeRouterOptions),
) (*gin.Engine, error) {
	options := sharedserver.RuntimeRouterOptions{
		APIKey:         apiKey,
		RegisterRoutes: TriggerRouteRegistrar(triggerHandler, apiKey),
	}
	applyRuntimeRouterOptions(&options, opts)

	out, err := sharedserver.NewRuntimeRouter(ctx, logger, &options)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

// TriggerRouteRegistrar는 trigger 내부 라우트를 API 키 인증 아래 등록하는 함수를 돌려준다. 인자 handler가 nil이면
// 아무것도 등록하지 않고, handler가 있는데 API 키가 비어 있으면 인증 없는 내부 라우트가 열리지 않도록 등록을 거부한다.
func TriggerRouteRegistrar(triggerHandler *TriggerHandler, apiKey string) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		if triggerHandler == nil {
			return nil
		}

		if strings.TrimSpace(apiKey) == "" {
			return errors.New("API_SECRET_KEY required")
		}

		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)

		return nil
	}
}

func applyRuntimeRouterOptions(options *sharedserver.RuntimeRouterOptions, opts []func(*sharedserver.RuntimeRouterOptions)) {
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
}
