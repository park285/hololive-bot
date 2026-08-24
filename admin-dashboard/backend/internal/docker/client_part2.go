package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/kapu/admin-dashboard/internal/httpx"
)

func dockerActionSucceeded(status int) bool {
	return status == http.StatusNotModified || (status >= 200 && status < 300)
}

func (c *Client) doAction(ctx context.Context, name, action string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/containers/"+url.PathEscape(name)+"/"+action, http.NoBody)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("create docker %s request: %w", action, err))
	}

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
