package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/hololive-shared/pkg/httpbody"
	"github.com/park285/shared-go/pkg/json"
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
)

type Client struct {
	baseURL             string
	http                *http.Client
	managedPrefixes     []string
	stopBlockedPrefixes []string
	excludeSuffixes     []string
	listTimeout         time.Duration
	actionTimeout       time.Duration

	mu       sync.RWMutex
	cachedAt time.Time
	cached   []Container
	cacheTTL time.Duration

	refreshMu sync.Mutex
	refresh   *containerListRefresh
}

func NewClient(dockerHost string) (*Client, error) {
	baseURL, transport, err := dockerHTTPTransport(dockerHost)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Transport: transport,
		},
		managedPrefixes:     []string{"hololive", "valkey", "postgres", "deunhealth", "admin"},
		stopBlockedPrefixes: []string{"valkey", "postgres", "deunhealth", "admin"},
		excludeSuffixes:     []string{"-init"},
		listTimeout:         10 * time.Second,
		actionTimeout:       (stopGraceSeconds + 10) * time.Second,
		cacheTTL:            5 * time.Second,
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
	if err := httpbody.DrainAndClose(resp.Body, httpbody.DefaultDrainLimit); err != nil {
		return false
	}
	return available
}

func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	if cached, ok := c.cachedContainers(); ok {
		return cached, nil
	}

	refresh, leader := c.beginListRefresh()
	if leader {
		return c.runListRefresh(ctx, refresh)
	}

	select {
	case <-ctx.Done():
		return nil, &httpx.AppError{
			Status: http.StatusServiceUnavailable,
			Body:   httpx.ErrorResponse{Error: "Docker service not available"},
			Cause:  ctx.Err(),
		}
	case <-refresh.done:
		return cloneContainers(refresh.containers), refresh.err
	}
}

func (c *Client) fetchAndMapContainers(ctx context.Context) ([]Container, error) {
	summaries, err := c.fetchContainerSummaries(ctx)
	if err != nil {
		return nil, err
	}
	containers := make([]Container, 0, len(summaries))
	for i := range summaries {
		if mapped, ok := c.mapContainer(&summaries[i]); ok {
			containers = append(containers, mapped)
		}
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	c.storeCache(containers)
	return containers, nil
}

func (c *Client) fetchContainerSummaries(ctx context.Context) ([]containerSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, c.listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/containers/json?all=true", http.NoBody)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	resp, err := c.http.Do(req) //nolint:bodyclose // ReadAllAndClose가 응답 body를 모든 반환 경로에서 닫는다.
	if err != nil || resp == nil {
		return nil, httpx.NewError(http.StatusServiceUnavailable, "Docker service not available")
	}
	body, err := httpbody.ReadAllAndClose(resp.Body, maxDockerListResponseBytes)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("read docker list containers response: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpx.Internal(fmt.Errorf("docker list containers returned %s", resp.Status))
	}
	var summaries []containerSummary
	if err := json.Unmarshal(body, &summaries); err != nil {
		return nil, httpx.Internal(err)
	}
	return summaries, nil
}

func (c *Client) RestartContainer(ctx context.Context, name string) error {
	return c.action(ctx, name, fmt.Sprintf("restart?t=%d", stopGraceSeconds), c.actionTimeout)
}

func (c *Client) StopContainer(ctx context.Context, name string) error {
	if !c.IsManaged(name) {
		return httpx.NewError(http.StatusNotFound, "container not found")
	}
	if c.stopBlocked(name) {
		return httpx.NewError(http.StatusForbidden, "stopping infrastructure container is not allowed; use restart")
	}
	return c.action(ctx, name, fmt.Sprintf("stop?t=%d", stopGraceSeconds), c.actionTimeout)
}

func (c *Client) StartContainer(ctx context.Context, name string) error {
	return c.action(ctx, name, "start", c.listTimeout)
}

func (c *Client) stopBlocked(name string) bool {
	for _, prefix := range c.stopBlockedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (c *Client) IsManaged(name string) bool {
	managed := false
	for _, prefix := range c.managedPrefixes {
		if strings.HasPrefix(name, prefix) {
			managed = true
			break
		}
	}
	if !managed {
		return false
	}
	for _, suffix := range c.excludeSuffixes {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

func (c *Client) action(ctx context.Context, name, action string, timeout time.Duration) error {
	if !c.IsManaged(name) {
		return httpx.NewError(http.StatusNotFound, "container not found")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := c.doAction(ctx, name, action) //nolint:bodyclose // DrainAndClose가 응답 body를 모든 반환 경로에서 닫는다.
	if err != nil {
		return err
	}
	if resp == nil {
		return httpx.NewError(http.StatusServiceUnavailable, "Docker service not available")
	}
	if err := httpbody.DrainAndClose(resp.Body, httpbody.DefaultDrainLimit); err != nil {
		return httpx.Internal(fmt.Errorf("close docker %s response: %w", action, err))
	}
	if resp.StatusCode == http.StatusNotFound {
		return httpx.NewError(http.StatusNotFound, "container not found")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpx.Internal(fmt.Errorf("docker %s %s returned %s", action, name, resp.Status))
	}
	c.clearCache()
	return nil
}

func (c *Client) doAction(ctx context.Context, name, action string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/containers/"+url.PathEscape(name)+"/"+action, http.NoBody)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, httpx.NewError(http.StatusServiceUnavailable, "Docker service not available")
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
		return "http://docker", &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}}, nil
	}
	if after, ok := strings.CutPrefix(dockerHost, "tcp://"); ok {
		return "http://" + after, http.DefaultTransport, nil
	}
	if strings.HasPrefix(dockerHost, "http://") || strings.HasPrefix(dockerHost, "https://") {
		return strings.TrimRight(dockerHost, "/"), http.DefaultTransport, nil
	}
	return "", nil, fmt.Errorf("unsupported DOCKER_HOST: %s", dockerHost)
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
