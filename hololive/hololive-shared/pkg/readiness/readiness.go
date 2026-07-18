package readiness

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const (
	defaultProbeTimeout = 2 * time.Second

	GroupDependencies = "dependencies"
	GroupEgressFlags  = "egress_flags"
)

type Check struct {
	Name  string
	Group string
	Probe func(context.Context) error
}

func PostgresCheck(db database.Client) Check {
	return Check{
		Name:  "postgres",
		Group: GroupDependencies,
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
		Group: GroupDependencies,
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

type Probe struct {
	name    string
	timeout time.Duration
	checks  []Check
}

func NewProbe(name string, checks ...Check) *Probe {
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
		name:    strings.TrimSpace(name),
		timeout: defaultProbeTimeout,
		checks:  filtered,
	}
}

func (p *Probe) Name() string {
	return p.name
}

// 동기 순차 실행은 의도적이다: gin RecoveryMiddleware 경계 안에서 돌려 probe
// panic이 프로세스를 죽이지 않게 하고, per-check fresh timeout으로 한 dependency
// 의 hang이 다른 dependency 판정을 오염시키지 않게 한다.
func (p *Probe) Evaluate(ctx context.Context) (ready bool, groups map[string]map[string]bool) {
	groups = map[string]map[string]bool{
		GroupDependencies: {},
		GroupEgressFlags:  {},
	}
	ready = true
	for _, check := range p.checks {
		ok := p.runCheck(ctx, check) == nil
		// NewProbe의 normalizeGroup 때문에 nil bucket은 실제로 나오지 않지만,
		// NilAway는 그 원거리 불변식을 증명하지 못해 이 지역 guard가 필요하다.
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
	case GroupEgressFlags:
		return GroupEgressFlags
	default:
		return GroupDependencies
	}
}

func HTTPStatus(ready bool) (statusCode int, status string) {
	if ready {
		return http.StatusOK, "ready"
	}
	return http.StatusServiceUnavailable, "not_ready"
}

func BasePayload(base health.Response, status string) map[string]any {
	return map[string]any{
		"status":     status,
		"version":    base.Version,
		"uptime":     base.Uptime,
		"goroutines": base.Goroutines,
	}
}

func RequestContext(fallback context.Context, c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}
