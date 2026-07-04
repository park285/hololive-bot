package delivery

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const runtimeIrisH3DialGuardResolveTimeout = 5 * time.Second

func newRuntimeIrisH3DialGuard(resolveBaseURL func() (string, error)) func(net.IP) error {
	return func(ip net.IP) error {
		if ip == nil {
			return fmt.Errorf("iris h3 egress denied: nil dial ip")
		}
		if resolveBaseURL == nil {
			return fmt.Errorf("iris h3 egress guard has no base url resolver")
		}

		baseURL, err := resolveBaseURL()
		if err != nil {
			return fmt.Errorf("resolve iris base url for h3 egress guard: %w", err)
		}
		host, allowed, err := resolveRuntimeIrisH3DialGuardIPs(baseURL)
		if err != nil {
			return err
		}
		for _, candidate := range allowed {
			if candidate.Equal(ip) {
				return nil
			}
		}
		return fmt.Errorf("iris h3 egress denied: dial ip %s not in allowset derived from iris base url host %q", ip, host)
	}
}

func resolveRuntimeIrisH3DialGuardIPs(baseURL string) (string, []net.IP, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", nil, fmt.Errorf("parse iris base url for h3 egress guard: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", nil, fmt.Errorf("iris base url has no host for h3 egress guard")
	}

	if literal := net.ParseIP(host); literal != nil {
		return host, []net.IP{literal}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), runtimeIrisH3DialGuardResolveTimeout)
	defer cancel()

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", nil, fmt.Errorf("resolve iris base url host %q for h3 egress guard: %w", host, err)
	}
	if len(ipAddrs) == 0 {
		return "", nil, fmt.Errorf("iris base url host %q resolved to no addresses for h3 egress guard", host)
	}

	allowed := make([]net.IP, 0, len(ipAddrs))
	for _, ipAddr := range ipAddrs {
		allowed = append(allowed, ipAddr.IP)
	}
	return host, allowed, nil
}
