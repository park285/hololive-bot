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
		RegisterRoutes: triggerRuntimeRouteRegistrar(triggerHandler, apiKey),
	}
	applyRuntimeRouterOptions(&options, opts)

	out, err := sharedserver.NewRuntimeRouter(ctx, logger, &options)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func triggerRuntimeRouteRegistrar(triggerHandler *TriggerHandler, apiKey string) func(*gin.Engine) error {
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
