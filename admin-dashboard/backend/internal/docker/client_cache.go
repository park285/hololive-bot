package docker

import (
	"context"
	"time"
)

type containerListRefresh struct {
	done       chan struct{}
	containers []Container
	err        error
}

func (c *Client) runListRefresh(ctx context.Context, refresh *containerListRefresh) ([]Container, error) {
	if cached, ok := c.cachedContainers(); ok {
		c.finishListRefresh(refresh, cached, nil)
		return cached, nil
	}
	containers, err := c.fetchAndMapContainers(ctx)
	c.finishListRefresh(refresh, containers, err)
	return cloneContainers(containers), err
}

func (c *Client) beginListRefresh() (*containerListRefresh, bool) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if c.refresh != nil {
		return c.refresh, false
	}
	refresh := &containerListRefresh{done: make(chan struct{})}
	c.refresh = refresh
	return refresh, true
}

func (c *Client) finishListRefresh(refresh *containerListRefresh, containers []Container, err error) {
	refresh.containers = cloneContainers(containers)
	refresh.err = err

	c.refreshMu.Lock()
	if c.refresh == refresh {
		c.refresh = nil
	}
	close(refresh.done)
	c.refreshMu.Unlock()
}

func (c *Client) cachedContainers() ([]Container, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.cachedAt) < c.cacheTTL && c.cached != nil {
		return cloneContainers(c.cached), true
	}
	return nil, false
}

func (c *Client) storeCache(containers []Container) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedAt = time.Now()
	c.cached = cloneContainers(containers)
}

func (c *Client) clearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached = nil
	c.cachedAt = time.Time{}
}

func cloneContainers(containers []Container) []Container {
	if containers == nil {
		return nil
	}
	cloned := make([]Container, len(containers))
	for i := range containers {
		cloned[i] = containers[i]
		if containers[i].Health != nil {
			health := *containers[i].Health
			cloned[i].Health = &health
		}
		cloned[i].Ports = clonePortMappings(containers[i].Ports)
	}
	return cloned
}

func clonePortMappings(ports []PortMapping) []PortMapping {
	if ports == nil {
		return nil
	}
	cloned := make([]PortMapping, len(ports))
	for i := range ports {
		cloned[i] = ports[i]
		if ports[i].PublicPort != nil {
			publicPort := *ports[i].PublicPort
			cloned[i].PublicPort = &publicPort
		}
	}
	return cloned
}
