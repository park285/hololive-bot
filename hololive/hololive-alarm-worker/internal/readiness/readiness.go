package readiness

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type (
	Check = sharedreadiness.Check
	Probe = sharedreadiness.Probe
)

func NewProbe(runtimeName string, checks ...Check) *Probe {
	return sharedreadiness.NewProbe(runtimeName, checks...)
}

func PostgresCheck(db database.Client) Check {
	return sharedreadiness.PostgresCheck(db)
}

func ValkeyCheck(client cache.Client) Check {
	return sharedreadiness.ValkeyCheck(client)
}

func BoolEnvNotFalseCheck(name, key string, defaultValue bool) Check {
	return Check{
		Name:  name,
		Group: sharedreadiness.GroupEgressFlags,
		Probe: func(context.Context) error {
			return checkBoolEnvNotFalse(key, defaultValue)
		},
	}
}

func ExplicitTrueBoolEnvCheck(name, key string) Check {
	return Check{
		Name:  name,
		Group: sharedreadiness.GroupEgressFlags,
		Probe: func(context.Context) error {
			value, explicit, err := lookupBoolEnv(key)
			if err != nil {
				return err
			}
			if !explicit || value == nil || !*value {
				return fmt.Errorf("%s must be true", key)
			}
			return nil
		},
	}
}

func PublicGinHandler(ctx context.Context, probe *Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := publicResponse(probe, sharedreadiness.RequestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func InternalGinHandler(ctx context.Context, probe *Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := internalResponse(probe, sharedreadiness.RequestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func internalResponse(probe *Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, runtimePayload(base, "not_ready", "")
	}

	ready, groups := probe.Evaluate(ctx)
	statusCode, status := sharedreadiness.HTTPStatus(ready)
	payload = runtimePayload(base, status, probe.Name())
	for group, checks := range groups {
		payload[group] = checks
	}
	return statusCode, payload
}

func publicResponse(probe *Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, runtimePayload(base, "not_ready", "")
	}
	ready, _ := probe.Evaluate(ctx)
	statusCode, status := sharedreadiness.HTTPStatus(ready)
	return statusCode, runtimePayload(base, status, probe.Name())
}

func runtimePayload(base health.Response, status, runtimeName string) map[string]any {
	payload := sharedreadiness.BasePayload(base, status)
	if strings.TrimSpace(runtimeName) != "" {
		payload["runtime"] = strings.TrimSpace(runtimeName)
	}
	return payload
}

func lookupBoolEnv(key string) (value *bool, explicit bool, err error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return nil, false, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, true, nil
	}
	parsed, parseErr := strconv.ParseBool(trimmed)
	if parseErr != nil {
		return nil, true, fmt.Errorf("%s must be boolean: %w", key, parseErr)
	}
	return &parsed, true, nil
}

func checkBoolEnvNotFalse(key string, defaultValue bool) error {
	value, _, err := lookupBoolEnv(key)
	if err != nil {
		return err
	}
	if value == nil {
		return checkDefaultBool(key, defaultValue)
	}
	if !*value {
		return fmt.Errorf("%s=false", key)
	}
	return nil
}

func checkDefaultBool(key string, defaultValue bool) error {
	if defaultValue {
		return nil
	}
	return fmt.Errorf("%s=false", key)
}
