package readiness

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const defaultProbeTimeout = 2 * time.Second

type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

func PostgresCheck(db database.Client) Check {
	return Check{
		Name: "postgres",
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
		Name: "valkey",
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
	plane   string
	timeout time.Duration
	checks  []Check
}

func NewProbe(plane string, checks ...Check) *Probe {
	filtered := make([]Check, 0, len(checks))
	for _, c := range checks {
		if c.Probe == nil {
			continue
		}
		filtered = append(filtered, c)
	}
	return &Probe{
		plane:   plane,
		timeout: defaultProbeTimeout,
		checks:  filtered,
	}
}

func Pick(probes ...*Probe) *Probe {
	for _, p := range probes {
		if p != nil {
			return p
		}
	}
	return nil
}

// 동기 순차 실행은 의도적이다: gin RecoveryMiddleware 경계 안에서 돌려 probe
// panic이 프로세스를 죽이지 않게 하고, per-check fresh timeout으로 한 dependency
// 의 hang이 다른 dependency 판정을 오염시키지 않게 한다.
func (p *Probe) Evaluate(ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	dependencies := make(map[string]bool, len(p.checks))
	ready := true
	for _, c := range p.checks {
		if err := p.runCheck(ctx, c); err != nil {
			ready = false
			dependencies[c.Name] = false
			continue
		}
		dependencies[c.Name] = true
	}

	status := "ready"
	statusCode = http.StatusOK
	if !ready {
		status = "not_ready"
		statusCode = http.StatusServiceUnavailable
	}

	return statusCode, map[string]any{
		"status":       status,
		"version":      base.Version,
		"uptime":       base.Uptime,
		"goroutines":   base.Goroutines,
		"plane":        p.plane,
		"dependencies": dependencies,
	}
}

func (p *Probe) runCheck(ctx context.Context, c Check) error {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return c.Probe(probeCtx)
}

func GinHandler(p *Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil {
			c.JSON(http.StatusOK, map[string]any{"status": "ready", "health": health.Get()})
			return
		}
		statusCode, payload := p.Evaluate(c.Request.Context())
		c.JSON(statusCode, payload)
	}
}
