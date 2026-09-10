package docker

import (
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/admin-dashboard/internal/contract"
)

type Container struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Image       string        `json:"image"`
	Status      string        `json:"status"`
	State       string        `json:"state"`
	Health      *string       `json:"health,omitempty"`
	Created     int64         `json:"created"`
	Ports       []PortMapping `json:"ports"`
	Managed     bool          `json:"managed"`
	StopBlocked bool          `json:"stopBlocked"`
}

type PortMapping struct {
	PrivatePort uint16  `json:"private_port"`
	PublicPort  *uint16 `json:"public_port,omitempty"`
	PortType    string  `json:"port_type"`
}

const (
	stopGraceSeconds           = 30
	maxDockerListResponseBytes = 8 << 20
	responseBodyDrainLimit     = 64 << 10
)

type Client struct {
	baseURL       string
	http          *http.Client
	listTimeout   time.Duration
	actionTimeout time.Duration

	mu       sync.RWMutex
	cachedAt time.Time
	cached   []Container
	cacheTTL time.Duration

	refreshMu       sync.Mutex
	refresh         *containerListRefresh
	cacheGeneration uint64
}

func NewClient(dockerHost string) (*Client, error) {
	baseURL, transport, err := dockerHTTPTransport(dockerHost)
	if err != nil {
		return nil, fmt.Errorf("docker HTTP transport: %w", err)
	}

	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		listTimeout:   10 * time.Second,
		actionTimeout: (stopGraceSeconds + 10) * time.Second,
		cacheTTL:      5 * time.Second,
	}, nil
}

func (c *Client) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, c.listTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", http.NoBody)
	if err != nil {
		return false
	}

	resp, err := c.http.Do(req) //nolint:bodyclose // DrainAndClose가 응답 body를 모든 반환 경로에서 닫는다.
	if err != nil || resp == nil {
		return false
	}

	available := resp.StatusCode >= 200 && resp.StatusCode < 300
	if err := httputil.DrainAndClose(resp.Body, responseBodyDrainLimit); err != nil {
		return false
	}

	return available
}

// Close는 요청 정리 후 이 client가 소유하는 유휴 연결을 닫습니다.
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	out, err := c.listContainers(ctx, true)
	if err != nil {
		return out, fmt.Errorf("list containers: %w", err)
	}

	return out, nil
}

func (c *Client) listContainers(ctx context.Context, retryCanceledRefresh bool) ([]Container, error) {
	if cached, ok := c.cachedContainers(); ok {
		return cached, nil
	}

	refresh, leader := c.beginListRefresh()
	containers, err := c.resolveListRefresh(ctx, refresh, leader)

	if retryCanceledRefresh && ctx.Err() == nil && errors.Is(err, context.Canceled) {
		retried, retryErr := c.listContainers(ctx, false)
		if retryErr != nil {
			return retried, fmt.Errorf("list containers: %w", retryErr)
		}

		return retried, nil
	}

	if err != nil {
		return containers, fmt.Errorf("resolve list refresh: %w", err)
	}

	return containers, nil
}

func (c *Client) resolveListRefresh(ctx context.Context, refresh *containerListRefresh, leader bool) ([]Container, error) {
	if leader {
		out, err := c.runListRefresh(ctx, refresh)
		if err != nil {
			return out, fmt.Errorf("run list refresh: %w", err)
		}

		return out, nil
	}

	out, err := waitForListRefresh(ctx, refresh)
	if err != nil {
		return out, fmt.Errorf("wait for list refresh: %w", err)
	}

	return out, nil
}

func waitForListRefresh(ctx context.Context, refresh *containerListRefresh) ([]Container, error) {
	select {
	case <-ctx.Done():
		return nil, &contract.AppError{
			Status: http.StatusServiceUnavailable,
			Body:   contract.ErrorResponse{Error: "Docker service not available"},
			Cause:  ctx.Err(),
		}
	case <-refresh.done:
		return cloneContainers(refresh.containers), refresh.err
	}
}

func (c *Client) fetchAndMapContainers(ctx context.Context) ([]Container, error) {
	summaries, err := c.fetchContainerSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch container summaries: %w", err)
	}

	containers := make([]Container, 0, len(summaries))
	for i := range summaries {
		if mapped, ok := c.mapContainer(&summaries[i]); ok {
			containers = append(containers, mapped)
		}
	}

	slices.SortFunc(containers, func(left, right Container) int {
		return cmp.Compare(left.Name, right.Name)
	})

	return containers, nil
}

func (c *Client) fetchContainerSummaries(ctx context.Context) ([]containerSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, c.listTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/containers/json?all=true", http.NoBody)
	if err != nil {
		return nil, contract.Internal(fmt.Errorf("create docker list containers request: %w", err))
	}

	resp, err := c.http.Do(req) //nolint:bodyclose // ReadAllAndClose가 응답 body를 모든 반환 경로에서 닫는다.
	if err != nil {
		return nil, dockerUnavailableError("list containers", err)
	}

	if resp == nil {
		return nil, dockerUnavailableError("list containers", nil)
	}

	body, err := httputil.ReadAllAndCloseWithDrainLimit(resp.Body, maxDockerListResponseBytes, responseBodyDrainLimit)
	if err != nil {
		return nil, contract.Internal(fmt.Errorf("read docker list containers response: %w", err))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, contract.Internal(fmt.Errorf("docker list containers returned %s", resp.Status))
	}

	var summaries []containerSummary

	if err := jsonv2.Unmarshal(body, &summaries); err != nil {
		return nil, contract.Internal(fmt.Errorf("decode docker list containers response: %w", err))
	}

	return summaries, nil
}

func dockerUnavailableError(operation string, cause error) *contract.AppError {
	err := contract.NewError(http.StatusServiceUnavailable, "Docker service not available")

	if cause != nil {
		err.Cause = fmt.Errorf("docker %s: %w", operation, cause)
	}

	return err
}

func (c *Client) RestartContainer(ctx context.Context, name string) error {
	if err := c.action(ctx, name, fmt.Sprintf("restart?t=%d", stopGraceSeconds), c.actionTimeout); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	return nil
}

func (c *Client) StopContainer(ctx context.Context, name string) error {
	if !c.IsManaged(name) {
		return contract.NewError(http.StatusNotFound, "container not found")
	}

	if c.stopBlocked(name) {
		return contract.NewError(http.StatusForbidden, "stopping infrastructure container is not allowed; use restart")
	}

	if err := c.action(ctx, name, fmt.Sprintf("stop?t=%d", stopGraceSeconds), c.actionTimeout); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	return nil
}

func (c *Client) StartContainer(ctx context.Context, name string) error {
	if err := c.action(ctx, name, "start", c.listTimeout); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	return nil
}

func (c *Client) stopBlocked(name string) bool {
	capabilities := containerActions[name]
	return capabilities != 0 && capabilities&2 == 0
}

// IsManaged는 중앙 전용 proxy 정책에 정확히 등록된 컨테이너 이름만 허용합니다.
func (c *Client) IsManaged(name string) bool { return containerActions[name] != 0 }

func allowedAction(name, action string) bool {
	required := map[string]uint8{"start": 1, "stop": 2, "restart": 4}[action]
	return required != 0 && containerActions[name]&required != 0
}

func (c *Client) action(ctx context.Context, name, action string, timeout time.Duration) error {
	if !c.IsManaged(name) {
		return contract.NewError(http.StatusNotFound, "container not found")
	}

	operation, _, _ := strings.Cut(action, "?")
	if !allowedAction(name, operation) {
		return contract.Forbidden()
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)

	defer cancel()

	resp, err := c.doAction(ctx, name, action) //nolint:bodyclose // DrainAndClose가 응답 body를 모든 반환 경로에서 닫는다.
	if err != nil {
		return fmt.Errorf("do action: %w", err)
	}

	if resp == nil {
		return dockerUnavailableError(action+" "+name, nil)
	}

	if err := httputil.DrainAndClose(resp.Body, responseBodyDrainLimit); err != nil {
		return contract.Internal(fmt.Errorf("close docker %s response: %w", action, err))
	}

	if resp.StatusCode == http.StatusNotFound {
		return contract.NewError(http.StatusNotFound, "container not found")
	}

	if !dockerActionSucceeded(operation, resp.StatusCode) {
		return contract.Internal(fmt.Errorf("docker %s %s returned %s", action, name, resp.Status))
	}

	c.clearCache()

	return nil
}

// Engine API v1.52는 204를 완료로, start/stop의 304만 이미 목표 상태로 정의합니다.
func dockerActionSucceeded(action string, status int) bool {
	return status == http.StatusNoContent || status == http.StatusNotModified && (action == "start" || action == "stop")
}

func (c *Client) doAction(ctx context.Context, name, action string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/containers/"+url.PathEscape(name)+"/"+action, http.NoBody)
	if err != nil {
		return nil, contract.Internal(fmt.Errorf("create docker %s request: %w", action, err))
	}

	contract.MarkDispatched(ctx)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, dockerUnavailableError(action+" "+name, err)
	}

	return resp, nil
}

func (c *Client) mapContainer(summary *containerSummary) (Container, bool) {
	if len(summary.Names) == 0 {
		return Container{}, false
	}

	name := strings.TrimPrefix(summary.Names[0], "/")
	if !c.IsManaged(name) {
		return Container{}, false
	}

	ports := make([]PortMapping, 0, len(summary.Ports))
	for _, port := range summary.Ports {
		var public *uint16

		if port.PublicPort != 0 {
			value := port.PublicPort

			public = &value
		}

		portType := port.Type
		if portType == "" {
			portType = "tcp"
		}

		ports = append(ports, PortMapping{PrivatePort: port.PrivatePort, PublicPort: public, PortType: portType})
	}

	health := parseHealth(summary.Status)

	return Container{
		ID:          summary.ID,
		Name:        name,
		Image:       summary.Image,
		Status:      summary.Status,
		State:       summary.State,
		Health:      health,
		Created:     summary.Created,
		Ports:       ports,
		Managed:     true,
		StopBlocked: c.stopBlocked(name),
	}, true
}

func dockerHTTPTransport(dockerHost string) (string, http.RoundTripper, error) {
	if after, ok := strings.CutPrefix(dockerHost, "unix://"); ok {
		socket := after

		//nolint:revive // unix 소켓으로 다이얼하므로 스킴은 자리표시자이고, TLS를 협상할 원격 피어 자체가 없다.
		return "http://docker", &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer

			return dialer.DialContext(ctx, "unix", socket)
		}}, nil
	}

	if after, ok := strings.CutPrefix(dockerHost, "tcp://"); ok {
		transport, err := cloneDefaultHTTPTransport()
		if err != nil {
			return "", nil, fmt.Errorf("clone default HTTP transport: %w", err)
		}

		return "http://" + after, transport, nil
	}

	if strings.HasPrefix(dockerHost, "http://") || strings.HasPrefix(dockerHost, "https://") {
		transport, err := cloneDefaultHTTPTransport()
		if err != nil {
			return "", nil, fmt.Errorf("clone default HTTP transport: %w", err)
		}

		return strings.TrimRight(dockerHost, "/"), transport, nil
	}

	return "", nil, errors.New("unsupported DOCKER_HOST scheme")
}

func cloneDefaultHTTPTransport() (*http.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("clone default HTTP transport: unexpected transport type %T", http.DefaultTransport)
	}

	return transport.Clone(), nil
}

func parseHealth(status string) *string {
	for _, health := range []string{"healthy", "unhealthy", "starting"} {
		if strings.Contains(status, "("+health+")") {
			value := health
			return &value
		}
	}

	return nil
}

type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	Status  string            `json:"Status"`
	State   string            `json:"State"`
	Created int64             `json:"Created"`
	Ports   []containerPort   `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
}

type containerPort struct {
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort"`
	Type        string `json:"Type"`
}
