package readiness

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const (
	defaultProbeTimeout = 2 * time.Second

	groupDependencies = "dependencies"
	groupEgressFlags  = "egress_flags"
)

type Check struct {
	Name  string
	Group string
	Probe func(context.Context) error
}

type Probe struct {
	runtimeName string
	timeout     time.Duration
	checks      []Check
}

func NewProbe(runtimeName string, checks ...Check) *Probe {
	filtered := make([]Check, 0, len(checks))
	for _, check := range checks {
		if check.Probe == nil {
			continue
		}
		check.Name = strings.TrimSpace(check.Name)
		if check.Name == "" {
			continue
		}
		check.Group = normalizeGroup(check.Group)
		filtered = append(filtered, check)
	}
	return &Probe{
		runtimeName: strings.TrimSpace(runtimeName),
		timeout:     defaultProbeTimeout,
		checks:      filtered,
	}
}

func PostgresCheck(db database.Client) Check {
	return Check{
		Name:  "postgres",
		Group: groupDependencies,
		Probe: func(ctx context.Context) error {
			if db == nil {
				return errors.New("postgres client not configured")
			}
			return db.Ping(ctx)
		},
	}
}

func ValkeyCheck(client cache.Client) Check {
	return Check{
		Name:  "valkey",
		Group: groupDependencies,
		Probe: func(ctx context.Context) error {
			if client == nil {
				return errors.New("valkey client not configured")
			}
			if !client.IsConnected(ctx) {
				return errors.New("valkey ping failed")
			}
			return nil
		},
	}
}

func BoolEnvNotFalseCheck(name, key string, defaultValue bool) Check {
	return Check{
		Name:  name,
		Group: groupEgressFlags,
		Probe: func(context.Context) error {
			return checkBoolEnvNotFalse(key, defaultValue)
		},
	}
}

func ExplicitTrueBoolEnvCheck(name, key string) Check {
	return Check{
		Name:  name,
		Group: groupEgressFlags,
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
		statusCode, payload := publicResponse(probe, requestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func InternalGinHandler(ctx context.Context, probe *Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := internalResponse(probe, requestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func requestContext(fallback context.Context, c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}

func internalResponse(probe *Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, basePayload(base, "not_ready", "")
	}

	ready, groups := probe.evaluate(ctx)
	statusCode, status := readinessHTTPStatus(ready)
	payload = basePayload(base, status, probe.runtimeName)
	for group, checks := range groups {
		payload[group] = checks
	}
	return statusCode, payload
}

func publicResponse(probe *Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, basePayload(base, "not_ready", "")
	}
	ready, _ := probe.evaluate(ctx)
	statusCode, status := readinessHTTPStatus(ready)
	return statusCode, basePayload(base, status, probe.runtimeName)
}

func (p *Probe) evaluate(ctx context.Context) (ready bool, groups map[string]map[string]bool) {
	groups = map[string]map[string]bool{
		groupDependencies: {},
		groupEgressFlags:  {},
	}
	ready = true
	for _, check := range p.checks {
		ok := p.runCheck(ctx, check) == nil
		groupChecks := groups[check.Group]
		if groupChecks == nil {
			groupChecks = map[string]bool{}
			groups[check.Group] = groupChecks
		}
		groupChecks[check.Name] = ok
		if !ok {
			ready = false
		}
	}
	return ready, groups
}

func (p *Probe) runCheck(ctx context.Context, check Check) error {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return check.Probe(probeCtx)
}

func normalizeGroup(group string) string {
	switch strings.TrimSpace(group) {
	case groupEgressFlags:
		return groupEgressFlags
	default:
		return groupDependencies
	}
}

func readinessHTTPStatus(ready bool) (statusCode int, status string) {
	if ready {
		return http.StatusOK, "ready"
	}
	return http.StatusServiceUnavailable, "not_ready"
}

func basePayload(base health.Response, status, runtimeName string) map[string]any {
	payload := map[string]any{
		"status":     status,
		"version":    base.Version,
		"uptime":     base.Uptime,
		"goroutines": base.Goroutines,
	}
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
